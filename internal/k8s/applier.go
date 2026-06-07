package k8s

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Applier creates Kubernetes resources in dependency order via the API server.
type Applier struct {
	client         kubernetes.Interface
	rolloutTimeout time.Duration
	pollInterval   time.Duration
}

// NewApplier wraps a clientset (or fake) for create operations.
func NewApplier(client kubernetes.Interface) *Applier {
	return &Applier{
		client:         client,
		rolloutTimeout: 2 * time.Minute,
		pollInterval:   2 * time.Second,
	}
}

// SetRolloutTimeout controls how long Apply waits for the Deployment to become available.
func (a *Applier) SetRolloutTimeout(timeout time.Duration) {
	a.rolloutTimeout = timeout
}

// Apply creates or updates all resources in the bundle; on failure, attempts best-effort rollback of newly created resources.
func (a *Applier) Apply(ctx context.Context, bundle *ManifestBundle) error {
	// Order matters: namespace and RBAC before workloads; networking after service.
	steps := []struct {
		name  string
		apply func(context.Context) (bool, error)
	}{
		{"Namespace", func(ctx context.Context) (bool, error) { return a.applyNamespace(ctx, bundle) }},
		{"ServiceAccount", func(ctx context.Context) (bool, error) { return a.applyServiceAccount(ctx, bundle) }},
		{"ConfigMap", func(ctx context.Context) (bool, error) { return a.applyConfigMap(ctx, bundle) }},
		{"Secret", func(ctx context.Context) (bool, error) { return a.applySecret(ctx, bundle) }},
		{"Role", func(ctx context.Context) (bool, error) { return a.applyRole(ctx, bundle) }},
		{"RoleBinding", func(ctx context.Context) (bool, error) { return a.applyRoleBinding(ctx, bundle) }},
		{"Deployment", func(ctx context.Context) (bool, error) { return a.applyDeployment(ctx, bundle) }},
		{"Service", func(ctx context.Context) (bool, error) { return a.applyService(ctx, bundle) }},
		{"Ingress", func(ctx context.Context) (bool, error) { return a.applyIngress(ctx, bundle) }},
		{"HorizontalPodAutoscaler", func(ctx context.Context) (bool, error) { return a.applyHPA(ctx, bundle) }},
		{"PodDisruptionBudget", func(ctx context.Context) (bool, error) { return a.applyPDB(ctx, bundle) }},
		{"NetworkPolicy", func(ctx context.Context) (bool, error) { return a.applyNetworkPolicy(ctx, bundle) }},
	}

	var created []string
	for _, step := range steps {
		wasCreated, err := step.apply(ctx)
		if err != nil {
			_ = a.rollback(ctx, bundle, created)
			return fmt.Errorf("%s: %w", step.name, err)
		}
		if wasCreated {
			created = append(created, step.name)
		}
	}
	if a.rolloutTimeout > 0 {
		if err := a.waitForDeployment(ctx, bundle); err != nil {
			return fmt.Errorf("deployment rollout: %w", err)
		}
	}
	return nil
}

// rollback deletes resources created so far, in reverse order (best effort).
func (a *Applier) rollback(ctx context.Context, bundle *ManifestBundle, applied []string) error {
	ns := bundle.Namespace.Name
	var errs []string
	for i := len(applied) - 1; i >= 0; i-- {
		switch applied[i] {
		case "NetworkPolicy":
			_ = a.client.NetworkingV1().NetworkPolicies(ns).Delete(ctx, bundle.NetworkPolicy.Name, metav1.DeleteOptions{})
		case "PodDisruptionBudget":
			_ = a.client.PolicyV1().PodDisruptionBudgets(ns).Delete(ctx, bundle.PDB.Name, metav1.DeleteOptions{})
		case "HorizontalPodAutoscaler":
			_ = a.client.AutoscalingV2().HorizontalPodAutoscalers(ns).Delete(ctx, bundle.HPA.Name, metav1.DeleteOptions{})
		case "Ingress":
			_ = a.client.NetworkingV1().Ingresses(ns).Delete(ctx, bundle.Ingress.Name, metav1.DeleteOptions{})
		case "Service":
			_ = a.client.CoreV1().Services(ns).Delete(ctx, bundle.Service.Name, metav1.DeleteOptions{})
		case "Deployment":
			_ = a.client.AppsV1().Deployments(ns).Delete(ctx, bundle.Deployment.Name, metav1.DeleteOptions{})
		case "RoleBinding":
			_ = a.client.RbacV1().RoleBindings(ns).Delete(ctx, bundle.RoleBinding.Name, metav1.DeleteOptions{})
		case "Role":
			_ = a.client.RbacV1().Roles(ns).Delete(ctx, bundle.Role.Name, metav1.DeleteOptions{})
		case "Secret":
			_ = a.client.CoreV1().Secrets(ns).Delete(ctx, bundle.Secret.Name, metav1.DeleteOptions{})
		case "ConfigMap":
			_ = a.client.CoreV1().ConfigMaps(ns).Delete(ctx, bundle.ConfigMap.Name, metav1.DeleteOptions{})
		case "ServiceAccount":
			_ = a.client.CoreV1().ServiceAccounts(ns).Delete(ctx, bundle.ServiceAccount.Name, metav1.DeleteOptions{})
		case "Namespace":
			_ = a.client.CoreV1().Namespaces().Delete(ctx, ns, metav1.DeleteOptions{})
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("rollback issues: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (a *Applier) applyNamespace(ctx context.Context, b *ManifestBundle) (bool, error) {
	existing, err := a.client.CoreV1().Namespaces().Get(ctx, b.Namespace.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err := a.client.CoreV1().Namespaces().Create(ctx, b.Namespace, metav1.CreateOptions{})
		return true, err
	}
	if err != nil {
		return false, err
	}
	desired := existing.DeepCopy()
	desired.Labels = mergeStringMaps(desired.Labels, b.Namespace.Labels)
	desired.Annotations = mergeStringMaps(desired.Annotations, b.Namespace.Annotations)
	_, err = a.client.CoreV1().Namespaces().Update(ctx, desired, metav1.UpdateOptions{})
	return false, err
}

func (a *Applier) applyServiceAccount(ctx context.Context, b *ManifestBundle) (bool, error) {
	ns := b.Namespace.Name
	existing, err := a.client.CoreV1().ServiceAccounts(ns).Get(ctx, b.ServiceAccount.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err := a.client.CoreV1().ServiceAccounts(ns).Create(ctx, b.ServiceAccount, metav1.CreateOptions{})
		return true, err
	}
	if err != nil {
		return false, err
	}
	desired := b.ServiceAccount.DeepCopy()
	desired.ResourceVersion = existing.ResourceVersion
	_, err = a.client.CoreV1().ServiceAccounts(ns).Update(ctx, desired, metav1.UpdateOptions{})
	return false, err
}

func (a *Applier) applyConfigMap(ctx context.Context, b *ManifestBundle) (bool, error) {
	ns := b.Namespace.Name
	existing, err := a.client.CoreV1().ConfigMaps(ns).Get(ctx, b.ConfigMap.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err := a.client.CoreV1().ConfigMaps(ns).Create(ctx, b.ConfigMap, metav1.CreateOptions{})
		return true, err
	}
	if err != nil {
		return false, err
	}
	desired := b.ConfigMap.DeepCopy()
	desired.ResourceVersion = existing.ResourceVersion
	_, err = a.client.CoreV1().ConfigMaps(ns).Update(ctx, desired, metav1.UpdateOptions{})
	return false, err
}

func (a *Applier) applySecret(ctx context.Context, b *ManifestBundle) (bool, error) {
	ns := b.Namespace.Name
	existing, err := a.client.CoreV1().Secrets(ns).Get(ctx, b.Secret.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err := a.client.CoreV1().Secrets(ns).Create(ctx, b.Secret, metav1.CreateOptions{})
		return true, err
	}
	if err != nil {
		return false, err
	}
	desired := b.Secret.DeepCopy()
	desired.ResourceVersion = existing.ResourceVersion
	_, err = a.client.CoreV1().Secrets(ns).Update(ctx, desired, metav1.UpdateOptions{})
	return false, err
}

func (a *Applier) applyRole(ctx context.Context, b *ManifestBundle) (bool, error) {
	ns := b.Namespace.Name
	existing, err := a.client.RbacV1().Roles(ns).Get(ctx, b.Role.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err := a.client.RbacV1().Roles(ns).Create(ctx, b.Role, metav1.CreateOptions{})
		return true, err
	}
	if err != nil {
		return false, err
	}
	desired := b.Role.DeepCopy()
	desired.ResourceVersion = existing.ResourceVersion
	_, err = a.client.RbacV1().Roles(ns).Update(ctx, desired, metav1.UpdateOptions{})
	return false, err
}

func (a *Applier) applyRoleBinding(ctx context.Context, b *ManifestBundle) (bool, error) {
	ns := b.Namespace.Name
	existing, err := a.client.RbacV1().RoleBindings(ns).Get(ctx, b.RoleBinding.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err := a.client.RbacV1().RoleBindings(ns).Create(ctx, b.RoleBinding, metav1.CreateOptions{})
		return true, err
	}
	if err != nil {
		return false, err
	}
	desired := b.RoleBinding.DeepCopy()
	desired.ResourceVersion = existing.ResourceVersion
	_, err = a.client.RbacV1().RoleBindings(ns).Update(ctx, desired, metav1.UpdateOptions{})
	return false, err
}

func (a *Applier) applyDeployment(ctx context.Context, b *ManifestBundle) (bool, error) {
	ns := b.Namespace.Name
	existing, err := a.client.AppsV1().Deployments(ns).Get(ctx, b.Deployment.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err := a.client.AppsV1().Deployments(ns).Create(ctx, b.Deployment, metav1.CreateOptions{})
		return true, err
	}
	if err != nil {
		return false, err
	}
	desired := b.Deployment.DeepCopy()
	desired.ResourceVersion = existing.ResourceVersion
	_, err = a.client.AppsV1().Deployments(ns).Update(ctx, desired, metav1.UpdateOptions{})
	return false, err
}

func (a *Applier) applyService(ctx context.Context, b *ManifestBundle) (bool, error) {
	ns := b.Namespace.Name
	existing, err := a.client.CoreV1().Services(ns).Get(ctx, b.Service.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err := a.client.CoreV1().Services(ns).Create(ctx, b.Service, metav1.CreateOptions{})
		return true, err
	}
	if err != nil {
		return false, err
	}
	desired := b.Service.DeepCopy()
	desired.ResourceVersion = existing.ResourceVersion
	desired.Spec.ClusterIP = existing.Spec.ClusterIP
	desired.Spec.ClusterIPs = existing.Spec.ClusterIPs
	desired.Spec.IPFamilies = existing.Spec.IPFamilies
	desired.Spec.IPFamilyPolicy = existing.Spec.IPFamilyPolicy
	desired.Spec.InternalTrafficPolicy = existing.Spec.InternalTrafficPolicy
	_, err = a.client.CoreV1().Services(ns).Update(ctx, desired, metav1.UpdateOptions{})
	return false, err
}

func (a *Applier) applyIngress(ctx context.Context, b *ManifestBundle) (bool, error) {
	ns := b.Namespace.Name
	existing, err := a.client.NetworkingV1().Ingresses(ns).Get(ctx, b.Ingress.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err := a.client.NetworkingV1().Ingresses(ns).Create(ctx, b.Ingress, metav1.CreateOptions{})
		return true, err
	}
	if err != nil {
		return false, err
	}
	desired := b.Ingress.DeepCopy()
	desired.ResourceVersion = existing.ResourceVersion
	_, err = a.client.NetworkingV1().Ingresses(ns).Update(ctx, desired, metav1.UpdateOptions{})
	return false, err
}

func (a *Applier) applyHPA(ctx context.Context, b *ManifestBundle) (bool, error) {
	ns := b.Namespace.Name
	existing, err := a.client.AutoscalingV2().HorizontalPodAutoscalers(ns).Get(ctx, b.HPA.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err := a.client.AutoscalingV2().HorizontalPodAutoscalers(ns).Create(ctx, b.HPA, metav1.CreateOptions{})
		return true, err
	}
	if err != nil {
		return false, err
	}
	desired := b.HPA.DeepCopy()
	desired.ResourceVersion = existing.ResourceVersion
	_, err = a.client.AutoscalingV2().HorizontalPodAutoscalers(ns).Update(ctx, desired, metav1.UpdateOptions{})
	return false, err
}

func (a *Applier) applyPDB(ctx context.Context, b *ManifestBundle) (bool, error) {
	ns := b.Namespace.Name
	existing, err := a.client.PolicyV1().PodDisruptionBudgets(ns).Get(ctx, b.PDB.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err := a.client.PolicyV1().PodDisruptionBudgets(ns).Create(ctx, b.PDB, metav1.CreateOptions{})
		return true, err
	}
	if err != nil {
		return false, err
	}
	desired := b.PDB.DeepCopy()
	desired.ResourceVersion = existing.ResourceVersion
	_, err = a.client.PolicyV1().PodDisruptionBudgets(ns).Update(ctx, desired, metav1.UpdateOptions{})
	return false, err
}

func (a *Applier) applyNetworkPolicy(ctx context.Context, b *ManifestBundle) (bool, error) {
	ns := b.Namespace.Name
	existing, err := a.client.NetworkingV1().NetworkPolicies(ns).Get(ctx, b.NetworkPolicy.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err := a.client.NetworkingV1().NetworkPolicies(ns).Create(ctx, b.NetworkPolicy, metav1.CreateOptions{})
		return true, err
	}
	if err != nil {
		return false, err
	}
	desired := b.NetworkPolicy.DeepCopy()
	desired.ResourceVersion = existing.ResourceVersion
	_, err = a.client.NetworkingV1().NetworkPolicies(ns).Update(ctx, desired, metav1.UpdateOptions{})
	return false, err
}

func (a *Applier) waitForDeployment(ctx context.Context, b *ManifestBundle) error {
	ctx, cancel := context.WithTimeout(ctx, a.rolloutTimeout)
	defer cancel()

	ticker := time.NewTicker(a.pollInterval)
	defer ticker.Stop()

	for {
		deployment, err := a.client.AppsV1().Deployments(b.Namespace.Name).Get(ctx, b.Deployment.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if deploymentAvailable(deployment) {
			return nil
		}

		select {
		case <-ctx.Done():
			return rolloutStatusError(deployment)
		case <-ticker.C:
		}
	}
}

func deploymentAvailable(deployment *appsv1.Deployment) bool {
	desired := int32(1)
	if deployment.Spec.Replicas != nil {
		desired = *deployment.Spec.Replicas
	}
	if deployment.Status.ObservedGeneration < deployment.Generation {
		return false
	}
	return deployment.Status.UpdatedReplicas >= desired && deployment.Status.AvailableReplicas >= desired
}

func rolloutStatusError(deployment *appsv1.Deployment) error {
	desired := int32(1)
	if deployment.Spec.Replicas != nil {
		desired = *deployment.Spec.Replicas
	}
	for _, condition := range deployment.Status.Conditions {
		if condition.Type == appsv1.DeploymentProgressing && condition.Status == "False" && condition.Message != "" {
			return fmt.Errorf("timed out waiting for rollout: %s", condition.Message)
		}
	}
	return fmt.Errorf("timed out waiting for rollout: %d/%d updated, %d/%d available", deployment.Status.UpdatedReplicas, desired, deployment.Status.AvailableReplicas, desired)
}

func mergeStringMaps(base, overlay map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(overlay))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overlay {
		out[k] = v
	}
	return out
}
