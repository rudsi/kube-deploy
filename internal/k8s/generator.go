// Manifest generation: typed k8s.io/api objects built from DeployRequest (no YAML templates).
package k8s

import (
	"errors"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	apivalidation "k8s.io/apimachinery/pkg/util/validation"

	"kube-deploy/internal/model"
)

// Standard labels/annotations applied to every generated resource for traceability.
const (
	annotationDeploymentID = "kube-deploy.io/deployment-id"
	annotationCreatedAt    = "kube-deploy.io/created-at"
	annotationAppName      = "kube-deploy.io/app-name"

	labelManagedBy = "app.kubernetes.io/managed-by"
	labelName      = "app.kubernetes.io/name"
	labelInstance  = "app.kubernetes.io/instance"
	labelComponent = "app.kubernetes.io/component"

	ingressControllerNamespace = "ingress-nginx"
	maxAppNameLength           = 51
	maxReplicas                = 100
)

// ValidationError marks user-correctable deploy request errors.
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

// IsValidationError reports whether err is a deploy request validation error.
func IsValidationError(err error) bool {
	var ve *ValidationError
	return errors.As(err, &ve)
}

func validationError(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

type resourceQuantities struct {
	cpuRequest    resource.Quantity
	cpuLimit      resource.Quantity
	memoryRequest resource.Quantity
	memoryLimit   resource.Quantity
}

// ManifestBundle holds all Kubernetes objects created for one deploy operation.
type ManifestBundle struct {
	Namespace      *corev1.Namespace
	ServiceAccount *corev1.ServiceAccount
	ConfigMap      *corev1.ConfigMap
	Secret         *corev1.Secret
	Role           *rbacv1.Role
	RoleBinding    *rbacv1.RoleBinding
	Deployment     *appsv1.Deployment
	Service        *corev1.Service
	Ingress        *networkingv1.Ingress
	HPA            *autoscalingv2.HorizontalPodAutoscaler
	PDB            *policyv1.PodDisruptionBudget
	NetworkPolicy  *networkingv1.NetworkPolicy
}

// Generate builds the full resource bundle and the effective ingress host name.
func Generate(id string, req model.DeployRequest, createdAt time.Time) (*ManifestBundle, string, error) {
	if len(id) < 8 {
		return nil, "", validationError("deployment id must be at least 8 characters")
	}

	// Resolve defaults for namespace, probes, resources, and ingress.
	ns := req.Namespace
	if ns == "" {
		ns = sanitizeName(req.AppName)
	}
	appName := sanitizeName(req.AppName)
	replicas := req.Replicas
	if replicas == 0 {
		replicas = 1
	}
	probePath := req.ProbePath
	if probePath == "" {
		probePath = "/"
	}
	cpuReq := defaultString(req.CPURequest, "100m")
	cpuLim := defaultString(req.CPULimit, "500m")
	memReq := defaultString(req.MemoryRequest, "128Mi")
	memLim := defaultString(req.MemoryLimit, "256Mi")
	ingressHost := req.IngressHost
	if ingressHost == "" {
		ingressHost = fmt.Sprintf("%s.%s.local", appName, id[:8])
	}
	ingressClass := req.IngressClassName
	if ingressClass == "" {
		ingressClass = "nginx"
	}

	quantities, err := validateRequest(req, appName, ns, replicas, probePath, ingressHost, ingressClass, cpuReq, cpuLim, memReq, memLim)
	if err != nil {
		return nil, "", err
	}

	// Recommended labels (https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/).
	labels := map[string]string{
		labelManagedBy: model.ManagedBy,
		labelName:      appName,
		labelInstance:  appName,
		labelComponent: "api",
	}
	annotations := map[string]string{
		annotationDeploymentID: id,
		annotationCreatedAt:    createdAt.UTC().Format(time.RFC3339),
		annotationAppName:      req.AppName,
	}

	saName := appName + "-sa"
	configName := appName + "-config"
	secretName := appName + "-secret"
	roleName := appName + "-role"
	roleBindingName := appName + "-rolebinding"
	deployName := appName
	serviceName := appName
	ingressName := appName + "-ingress"
	hpaName := appName + "-hpa"
	pdbName := appName + "-pdb"
	netPolName := appName + "-netpol"

	configData := map[string]string{
		"APP_NAME":      req.AppName,
		"DEPLOYMENT_ID": id,
	}
	for k, v := range req.ConfigData {
		configData[k] = v
	}

	envVars := []corev1.EnvVar{
		{Name: "APP_NAME", Value: req.AppName},
		{Name: "DEPLOYMENT_ID", Value: id},
		{Name: "NAMESPACE", Value: ns},
	}
	for k, v := range req.Env {
		envVars = append(envVars, corev1.EnvVar{Name: k, Value: v})
	}

	port := req.Port
	containerPort := corev1.ContainerPort{
		Name:          "http",
		ContainerPort: port,
		Protocol:      corev1.ProtocolTCP,
	}

	// Shared HTTP probe for liveness and readiness on the app port.
	probe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path:   probePath,
				Port:   intstr.FromString("http"),
				Scheme: corev1.URISchemeHTTP,
			},
		},
		InitialDelaySeconds: 5,
		PeriodSeconds:       10,
		TimeoutSeconds:      3,
		FailureThreshold:    3,
	}

	// Hardened pod and container security (non-root, no privilege escalation, read-only root where possible).
	podSecurity := &corev1.PodSecurityContext{
		RunAsNonRoot: boolPtr(true),
		RunAsUser:    int64Ptr(10001),
		FSGroup:      int64Ptr(10001),
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
	containerSecurity := &corev1.SecurityContext{
		AllowPrivilegeEscalation: boolPtr(false),
		ReadOnlyRootFilesystem:   boolPtr(true),
		RunAsNonRoot:             boolPtr(true),
		RunAsUser:                int64Ptr(10001),
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
	}

	// Assemble namespace-scoped platform resources and the application workload.
	bundle := &ManifestBundle{
		Namespace: &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:        ns,
				Labels:      labels,
				Annotations: annotations,
			},
		},
		ServiceAccount: &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:        saName,
				Namespace:   ns,
				Labels:      labels,
				Annotations: annotations,
			},
			AutomountServiceAccountToken: boolPtr(false),
		},
		ConfigMap: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:        configName,
				Namespace:   ns,
				Labels:      labels,
				Annotations: annotations,
			},
			Data: configData,
		},
		Secret: &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: ns,
				Labels:    labels,
				Annotations: mergeAnnotations(annotations, map[string]string{
					"kube-deploy.io/secret-placeholder": "true",
				}),
			},
			Type: corev1.SecretTypeOpaque,
			StringData: map[string]string{
				"PLACEHOLDER": "replace-me",
			},
		},
		Role: &rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{
				Name:        roleName,
				Namespace:   ns,
				Labels:      labels,
				Annotations: annotations,
			},
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups:     []string{""},
					Resources:     []string{"configmaps", "secrets"},
					ResourceNames: []string{configName, secretName},
					Verbs:         []string{"get"},
				},
			},
		},
		RoleBinding: &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:        roleBindingName,
				Namespace:   ns,
				Labels:      labels,
				Annotations: annotations,
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:      rbacv1.ServiceAccountKind,
					Name:      saName,
					Namespace: ns,
				},
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     "Role",
				Name:     roleName,
			},
		},
		Deployment: &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:        deployName,
				Namespace:   ns,
				Labels:      labels,
				Annotations: annotations,
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: int32Ptr(replicas),
				Selector: &metav1.LabelSelector{
					MatchLabels: selectorLabels(labels),
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels:      selectorLabels(labels),
						Annotations: annotations,
					},
					Spec: corev1.PodSpec{
						ServiceAccountName:           saName,
						AutomountServiceAccountToken: boolPtr(false),
						SecurityContext:              podSecurity,
						Containers: []corev1.Container{
							{
								Name:            appName,
								Image:           req.Image,
								ImagePullPolicy: corev1.PullIfNotPresent,
								Ports:           []corev1.ContainerPort{containerPort},
								Env:             envVars,
								EnvFrom: []corev1.EnvFromSource{
									{
										ConfigMapRef: &corev1.ConfigMapEnvSource{
											LocalObjectReference: corev1.LocalObjectReference{Name: configName},
										},
									},
								},
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    quantities.cpuRequest,
										corev1.ResourceMemory: quantities.memoryRequest,
									},
									Limits: corev1.ResourceList{
										corev1.ResourceCPU:    quantities.cpuLimit,
										corev1.ResourceMemory: quantities.memoryLimit,
									},
								},
								LivenessProbe:   probe,
								ReadinessProbe:  probe,
								SecurityContext: containerSecurity,
								VolumeMounts: []corev1.VolumeMount{
									{Name: "tmp", MountPath: "/tmp"},
								},
							},
						},
						Volumes: []corev1.Volume{
							{
								Name: "tmp",
								VolumeSource: corev1.VolumeSource{
									EmptyDir: &corev1.EmptyDirVolumeSource{},
								},
							},
						},
					},
				},
			},
		},
		Service: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:        serviceName,
				Namespace:   ns,
				Labels:      labels,
				Annotations: annotations,
			},
			Spec: corev1.ServiceSpec{
				Type:     corev1.ServiceTypeClusterIP,
				Selector: selectorLabels(labels),
				Ports: []corev1.ServicePort{
					{
						Name:       "http",
						Port:       port,
						TargetPort: intstr.FromString("http"),
						Protocol:   corev1.ProtocolTCP,
					},
				},
			},
		},
		Ingress: &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ingressName,
				Namespace: ns,
				Labels:    labels,
				Annotations: mergeAnnotations(annotations, map[string]string{
					"kubernetes.io/ingress.class": ingressClass,
				}),
			},
			Spec: networkingv1.IngressSpec{
				IngressClassName: strPtr(ingressClass),
				Rules: []networkingv1.IngressRule{
					{
						Host: ingressHost,
						IngressRuleValue: networkingv1.IngressRuleValue{
							HTTP: &networkingv1.HTTPIngressRuleValue{
								Paths: []networkingv1.HTTPIngressPath{
									{
										Path:     "/",
										PathType: pathTypePrefix(),
										Backend: networkingv1.IngressBackend{
											Service: &networkingv1.IngressServiceBackend{
												Name: serviceName,
												Port: networkingv1.ServiceBackendPort{
													Number: port,
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		HPA: &autoscalingv2.HorizontalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Name:        hpaName,
				Namespace:   ns,
				Labels:      labels,
				Annotations: annotations,
			},
			Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
				ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
					Kind: "Deployment",
					Name: deployName,
				},
				MinReplicas: int32Ptr(maxInt32(1, replicas)),
				MaxReplicas: maxInt32(replicas*2, 3),
				Metrics: []autoscalingv2.MetricSpec{
					{
						Type: autoscalingv2.ResourceMetricSourceType,
						Resource: &autoscalingv2.ResourceMetricSource{
							Name: corev1.ResourceCPU,
							Target: autoscalingv2.MetricTarget{
								Type:               autoscalingv2.UtilizationMetricType,
								AverageUtilization: int32Ptr(70),
							},
						},
					},
				},
			},
		},
		PDB: &policyv1.PodDisruptionBudget{
			ObjectMeta: metav1.ObjectMeta{
				Name:        pdbName,
				Namespace:   ns,
				Labels:      labels,
				Annotations: annotations,
			},
			Spec: policyv1.PodDisruptionBudgetSpec{
				MinAvailable: intStrFromInt(maxInt32(1, replicas-1)),
				Selector: &metav1.LabelSelector{
					MatchLabels: selectorLabels(labels),
				},
			},
		},
		NetworkPolicy: &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:        netPolName,
				Namespace:   ns,
				Labels:      labels,
				Annotations: annotations,
			},
			Spec: networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{
					MatchLabels: selectorLabels(labels),
				},
				PolicyTypes: []networkingv1.PolicyType{
					networkingv1.PolicyTypeIngress,
					networkingv1.PolicyTypeEgress,
				},
				Ingress: []networkingv1.NetworkPolicyIngressRule{
					{
						From: []networkingv1.NetworkPolicyPeer{
							{
								NamespaceSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"kubernetes.io/metadata.name": ns,
									},
								},
								PodSelector: &metav1.LabelSelector{
									MatchLabels: selectorLabels(labels),
								},
							},
						},
					},
					{
						From: []networkingv1.NetworkPolicyPeer{
							{
								NamespaceSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"kubernetes.io/metadata.name": ingressControllerNamespace,
									},
								},
							},
						},
					},
				},
				Egress: []networkingv1.NetworkPolicyEgressRule{
					{
						To: []networkingv1.NetworkPolicyPeer{
							{
								NamespaceSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"kubernetes.io/metadata.name": ns,
									},
								},
							},
						},
					},
					{
						To: []networkingv1.NetworkPolicyPeer{
							{
								NamespaceSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"kubernetes.io/metadata.name": "kube-system",
									},
								},
							},
						},
						Ports: []networkingv1.NetworkPolicyPort{
							{Protocol: protocolUDP(), Port: intStrFromInt(53)},
							{Protocol: protocolTCP(), Port: intStrFromInt(53)},
						},
					},
				},
			},
		},
	}

	return bundle, ingressHost, nil
}

// validateRequest checks required fields before any objects are built.
func validateRequest(req model.DeployRequest, appName, ns string, replicas int32, probePath, ingressHost, ingressClass, cpuReq, cpuLim, memReq, memLim string) (*resourceQuantities, error) {
	if strings.TrimSpace(req.AppName) == "" {
		return nil, validationError("appName is required")
	}
	if appName == "" {
		return nil, validationError("appName must contain at least one DNS-safe character")
	}
	if len(appName) > maxAppNameLength {
		return nil, validationError("appName must be %d characters or fewer after sanitization", maxAppNameLength)
	}
	if err := validateDNS1123Label("appName", appName); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Image) == "" {
		return nil, validationError("image is required")
	}
	if strings.ContainsAny(req.Image, " \t\r\n") {
		return nil, validationError("image must not contain whitespace")
	}
	if req.Port < 1 || req.Port > 65535 {
		return nil, validationError("port must be between 1 and 65535")
	}
	if err := validateDNS1123Label("namespace", ns); err != nil {
		return nil, err
	}
	if replicas < 1 || replicas > maxReplicas {
		return nil, validationError("replicas must be between 1 and %d", maxReplicas)
	}
	if !strings.HasPrefix(probePath, "/") {
		return nil, validationError("probePath must start with /")
	}
	if err := validateDNS1123Subdomain("ingressHost", ingressHost); err != nil {
		return nil, err
	}
	if err := validateDNS1123Subdomain("ingressClassName", ingressClass); err != nil {
		return nil, err
	}
	for k := range req.Env {
		if errs := apivalidation.IsEnvVarName(k); len(errs) > 0 {
			return nil, validationError("env key %q is invalid: %s", k, strings.Join(errs, "; "))
		}
	}
	for k := range req.ConfigData {
		if errs := apivalidation.IsConfigMapKey(k); len(errs) > 0 {
			return nil, validationError("configData key %q is invalid: %s", k, strings.Join(errs, "; "))
		}
	}

	quantities, err := parseResourceQuantities(cpuReq, cpuLim, memReq, memLim)
	if err != nil {
		return nil, err
	}
	if quantities.cpuLimit.Cmp(quantities.cpuRequest) < 0 {
		return nil, validationError("cpuLimit must be greater than or equal to cpuRequest")
	}
	if quantities.memoryLimit.Cmp(quantities.memoryRequest) < 0 {
		return nil, validationError("memoryLimit must be greater than or equal to memoryRequest")
	}
	return quantities, nil
}

// sanitizeName converts app names to DNS-safe lowercase identifiers.
func sanitizeName(name string) string {
	out := strings.ToLower(strings.TrimSpace(name))
	out = strings.ReplaceAll(out, "_", "-")
	var b strings.Builder
	for _, r := range out {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else if r == '.' {
			b.WriteRune('-')
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return ""
	}
	return s
}

// selectorLabels are the labels used by Service, Deployment selector, and NetworkPolicy.
func selectorLabels(labels map[string]string) map[string]string {
	return map[string]string{
		labelManagedBy: labels[labelManagedBy],
		labelName:      labels[labelName],
		labelInstance:  labels[labelInstance],
		labelComponent: labels[labelComponent],
	}
}

func mergeAnnotations(base, extra map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func defaultString(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func parseResourceQuantities(cpuReq, cpuLim, memReq, memLim string) (*resourceQuantities, error) {
	cpuReqQty, err := parseQuantity("cpuRequest", cpuReq)
	if err != nil {
		return nil, err
	}
	cpuLimQty, err := parseQuantity("cpuLimit", cpuLim)
	if err != nil {
		return nil, err
	}
	memReqQty, err := parseQuantity("memoryRequest", memReq)
	if err != nil {
		return nil, err
	}
	memLimQty, err := parseQuantity("memoryLimit", memLim)
	if err != nil {
		return nil, err
	}
	return &resourceQuantities{
		cpuRequest:    cpuReqQty,
		cpuLimit:      cpuLimQty,
		memoryRequest: memReqQty,
		memoryLimit:   memLimQty,
	}, nil
}

func parseQuantity(field, v string) (resource.Quantity, error) {
	q, err := resource.ParseQuantity(v)
	if err != nil {
		return resource.Quantity{}, validationError("%s must be a valid Kubernetes resource quantity", field)
	}
	return q, nil
}

func validateDNS1123Label(field, value string) error {
	if errs := apivalidation.IsDNS1123Label(value); len(errs) > 0 {
		return validationError("%s %q is invalid: %s", field, value, strings.Join(errs, "; "))
	}
	return nil
}

func validateDNS1123Subdomain(field, value string) error {
	if errs := apivalidation.IsDNS1123Subdomain(value); len(errs) > 0 {
		return validationError("%s %q is invalid: %s", field, value, strings.Join(errs, "; "))
	}
	return nil
}

func boolPtr(v bool) *bool    { return &v }
func int64Ptr(v int64) *int64 { return &v }
func int32Ptr(v int32) *int32 { return &v }
func strPtr(v string) *string { return &v }

func pathTypePrefix() *networkingv1.PathType {
	t := networkingv1.PathTypePrefix
	return &t
}

func intStrFromInt(v int32) *intstr.IntOrString {
	s := intstr.FromInt(int(v))
	return &s
}

func protocolTCP() *corev1.Protocol {
	p := corev1.ProtocolTCP
	return &p
}

func protocolUDP() *corev1.Protocol {
	p := corev1.ProtocolUDP
	return &p
}

func maxInt32(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}

// ResourceNames returns human-readable resource identifiers for API responses.
func (b *ManifestBundle) ResourceNames() []string {
	return []string{
		"Namespace/" + b.Namespace.Name,
		"ServiceAccount/" + b.ServiceAccount.Name,
		"ConfigMap/" + b.ConfigMap.Name,
		"Secret/" + b.Secret.Name,
		"Role/" + b.Role.Name,
		"RoleBinding/" + b.RoleBinding.Name,
		"Deployment/" + b.Deployment.Name,
		"Service/" + b.Service.Name,
		"Ingress/" + b.Ingress.Name,
		"HorizontalPodAutoscaler/" + b.HPA.Name,
		"PodDisruptionBudget/" + b.PDB.Name,
		"NetworkPolicy/" + b.NetworkPolicy.Name,
	}
}

// Normalize applies defaults to the deploy request before persistence.
func Normalize(req *model.DeployRequest) {
	req.AppName = strings.TrimSpace(req.AppName)
	req.Namespace = strings.TrimSpace(req.Namespace)
	req.Image = strings.TrimSpace(req.Image)
	req.ProbePath = strings.TrimSpace(req.ProbePath)
	req.IngressHost = strings.TrimSpace(req.IngressHost)
	req.IngressClassName = strings.TrimSpace(req.IngressClassName)
	req.CPURequest = strings.TrimSpace(req.CPURequest)
	req.CPULimit = strings.TrimSpace(req.CPULimit)
	req.MemoryRequest = strings.TrimSpace(req.MemoryRequest)
	req.MemoryLimit = strings.TrimSpace(req.MemoryLimit)
	if req.Replicas == 0 {
		req.Replicas = 1
	}
	if req.Namespace == "" {
		req.Namespace = sanitizeName(req.AppName)
	}
	if req.ProbePath == "" {
		req.ProbePath = "/"
	}
	if req.CPURequest == "" {
		req.CPURequest = "100m"
	}
	if req.CPULimit == "" {
		req.CPULimit = "500m"
	}
	if req.MemoryRequest == "" {
		req.MemoryRequest = "128Mi"
	}
	if req.MemoryLimit == "" {
		req.MemoryLimit = "256Mi"
	}
	if req.IngressClassName == "" {
		req.IngressClassName = "nginx"
	}
}
