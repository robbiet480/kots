package kotsadm

import (
	"bytes"
	"time"

	"github.com/pkg/errors"
	kotsv1beta1 "github.com/replicatedhq/kots/kotskinds/apis/kots/v1beta1"
	"github.com/replicatedhq/kots/pkg/k8sutil"
	"github.com/replicatedhq/kots/pkg/kotsadm/types"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	kuberneteserrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
)

var timeoutWaitingForKotsadm = time.Duration(time.Minute * 2)

func getKotsadmYAML(deployOptions types.DeployOptions) (map[string][]byte, error) {
	docs := map[string][]byte{}
	s := json.NewYAMLSerializer(json.DefaultMetaFactory, scheme.Scheme, scheme.Scheme)

	var role bytes.Buffer
	if err := s.Encode(kotsadmRole(deployOptions.Namespace), &role); err != nil {
		return nil, errors.Wrap(err, "failed to marshal kotsadm role")
	}
	docs["kotsadm-role.yaml"] = role.Bytes()

	var roleBinding bytes.Buffer
	if err := s.Encode(kotsadmRoleBinding(deployOptions.Namespace), &roleBinding); err != nil {
		return nil, errors.Wrap(err, "failed to marshal kotsadm role binding")
	}
	docs["kotsadm-rolebinding.yaml"] = roleBinding.Bytes()

	var serviceAccount bytes.Buffer
	if err := s.Encode(kotsadmServiceAccount(deployOptions.Namespace), &serviceAccount); err != nil {
		return nil, errors.Wrap(err, "failed to marshal kotsadm service account")
	}
	docs["kotsadm-serviceaccount.yaml"] = serviceAccount.Bytes()

	var deployment bytes.Buffer
	if err := s.Encode(kotsadmDeployment(deployOptions), &deployment); err != nil {
		return nil, errors.Wrap(err, "failed to marshal kotsadm deployment")
	}
	docs["kotsadm-deployment.yaml"] = deployment.Bytes()

	var service bytes.Buffer
	if err := s.Encode(kotsadmService(deployOptions.Namespace), &service); err != nil {
		return nil, errors.Wrap(err, "failed to marshal kotsadm service")
	}
	docs["kotsadm-service.yaml"] = service.Bytes()

	return docs, nil
}

func waitForKotsadm(deployOptions *types.DeployOptions, clientset *kubernetes.Clientset) error {
	start := time.Now()

	for {
		pods, err := clientset.CoreV1().Pods(deployOptions.Namespace).List(metav1.ListOptions{LabelSelector: "app=kotsadm"})
		if err != nil {
			return errors.Wrap(err, "failed to list pods")
		}

		for _, pod := range pods.Items {
			if pod.Status.Phase == corev1.PodRunning {
				if pod.Status.ContainerStatuses[0].Ready == true {
					return nil
				}
			}
		}

		time.Sleep(time.Second)

		if time.Now().Sub(start) > timeoutWaitingForKotsadm {
			return errors.New("timeout waiting for kotsadm pod")
		}
	}
}

func ensureKotsadmComponent(deployOptions *types.DeployOptions, clientset *kubernetes.Clientset) error {
	if err := ensureKotsadmRBAC(*deployOptions, clientset); err != nil {
		return errors.Wrap(err, "failed to ensure kotsadm rbac")
	}

	if err := ensureApplicationMetadata(*deployOptions, clientset); err != nil {
		return errors.Wrap(err, "failed to ensure custom branding")
	}
	if err := ensureKotsadmDeployment(*deployOptions, clientset); err != nil {
		return errors.Wrap(err, "failed to ensure kotsadm deployment")
	}

	if err := ensureKotsadmService(deployOptions.Namespace, clientset); err != nil {
		return errors.Wrap(err, "failed to ensure kotsadm service")
	}

	return nil
}

func ensureKotsadmRBAC(deployOptions types.DeployOptions, clientset *kubernetes.Clientset) error {
	isClusterScoped, err := isKotsadmClusterScoped(deployOptions.ApplicationMetadata)
	if err != nil {
		return errors.Wrap(err, "failed to check if kotsadm is cluster scoped")
	}

	if isClusterScoped {
		return ensureKotsadmClusterRBAC(deployOptions, clientset)
	}

	if err := ensureKotsadmRole(deployOptions.Namespace, clientset); err != nil {
		return errors.Wrap(err, "failed to ensure kotsadm role")
	}

	if err := ensureKotsadmRoleBinding(deployOptions.Namespace, clientset); err != nil {
		return errors.Wrap(err, "failed to ensure kotsadm role binding")
	}

	if err := ensureKotsadmServiceAccount(deployOptions.Namespace, clientset); err != nil {
		return errors.Wrap(err, "failed to ensure kotsadm service account")
	}

	return nil
}

// ensureKotsadmClusterRBAC will ensure that the cluster role and cluster role bindings exists
func ensureKotsadmClusterRBAC(deployOptions types.DeployOptions, clientset *kubernetes.Clientset) error {
	err := ensureKotsadmClusterRole(clientset)
	if err != nil {
		return errors.Wrap(err, "failed to ensure kotsadm cluster role")
	}

	if err := ensureKotsadmClusterRoleBinding(deployOptions.Namespace, clientset); err != nil {
		return errors.Wrap(err, "failed to ensure kotsadm cluster role binding")
	}

	if err := ensureKotsadmServiceAccount(deployOptions.Namespace, clientset); err != nil {
		return errors.Wrap(err, "failed to ensure kotsadm service account")
	}

	return nil
}

func ensureKotsadmClusterRole(clientset *kubernetes.Clientset) error {
	_, err := clientset.RbacV1().ClusterRoles().Create(kotsadmClusterRole())
	if err == nil || kuberneteserrors.IsAlreadyExists(err) {
		return nil
	}

	return errors.Wrap(err, "failed to create cluster role")
}

func ensureKotsadmClusterRoleBinding(serviceAccountNamespace string, clientset *kubernetes.Clientset) error {
	clusterRoleBinding, err := clientset.RbacV1().ClusterRoleBindings().Get("kotsadm-rolebinding", metav1.GetOptions{})
	if kuberneteserrors.IsNotFound(err) {
		_, err := clientset.RbacV1().ClusterRoleBindings().Create(kotsadmClusterRoleBinding(serviceAccountNamespace))
		if err != nil {
			return errors.Wrap(err, "failed to create cluster rolebinding")
		}
		return nil
	} else if err != nil {
		return errors.Wrap(err, "failed to get cluster rolebinding")
	}

	for _, subject := range clusterRoleBinding.Subjects {
		if subject.Namespace == serviceAccountNamespace && subject.Name == "kotsadm" && subject.Kind == "ServiceAccount" {
			return nil
		}
	}

	clusterRoleBinding.Subjects = append(clusterRoleBinding.Subjects, rbacv1.Subject{
		Kind:      "ServiceAccount",
		Name:      "kotsadm",
		Namespace: serviceAccountNamespace,
	})

	_, err = clientset.RbacV1().ClusterRoleBindings().Update(clusterRoleBinding)
	if err != nil {
		return errors.Wrap(err, "failed to create cluster rolebinding")
	}

	return nil
}

func ensureKotsadmRole(namespace string, clientset *kubernetes.Clientset) error {
	currentRole, err := clientset.RbacV1().Roles(namespace).Get("kotsadm-role", metav1.GetOptions{})
	if err != nil {
		if !kuberneteserrors.IsNotFound(err) {
			return errors.Wrap(err, "failed to get role")
		}

		_, err := clientset.RbacV1().Roles(namespace).Create(kotsadmRole(namespace))
		if err != nil {
			return errors.Wrap(err, "failed to create role")
		}
		return nil
	}

	// we have now changed the role, so an upgrade is required
	k8sutil.UpdateRole(currentRole, kotsadmRole(namespace))
	_, err = clientset.RbacV1().Roles(namespace).Update(currentRole)
	if err != nil {
		return errors.Wrap(err, "failed to update role")
	}

	return nil
}

func ensureKotsadmRoleBinding(namespace string, clientset *kubernetes.Clientset) error {
	_, err := clientset.RbacV1().RoleBindings(namespace).Get("kotsadm-rolebinding", metav1.GetOptions{})
	if err != nil {
		if !kuberneteserrors.IsNotFound(err) {
			return errors.Wrap(err, "failed to get rolebinding")
		}

		_, err := clientset.RbacV1().RoleBindings(namespace).Create(kotsadmRoleBinding(namespace))
		if err != nil {
			return errors.Wrap(err, "failed to create rolebinding")
		}
	}

	return nil
}

func ensureKotsadmServiceAccount(namespace string, clientset *kubernetes.Clientset) error {
	_, err := clientset.CoreV1().ServiceAccounts(namespace).Get("kotsadm", metav1.GetOptions{})
	if err != nil {
		if !kuberneteserrors.IsNotFound(err) {
			return errors.Wrap(err, "failed to get serviceaccouont")
		}

		_, err := clientset.CoreV1().ServiceAccounts(namespace).Create(kotsadmServiceAccount(namespace))
		if err != nil {
			return errors.Wrap(err, "failed to create serviceaccount")
		}
	}

	return nil
}

func ensureKotsadmDeployment(deployOptions types.DeployOptions, clientset *kubernetes.Clientset) error {
	existingDeployment, err := clientset.AppsV1().Deployments(deployOptions.Namespace).Get("kotsadm", metav1.GetOptions{})
	if err != nil {
		if !kuberneteserrors.IsNotFound(err) {
			return errors.Wrap(err, "failed to get existing deployment")
		}

		_, err := clientset.AppsV1().Deployments(deployOptions.Namespace).Create(kotsadmDeployment(deployOptions))
		if err != nil {
			return errors.Wrap(err, "failed to create deployment")
		}
		return nil
	}

	if err = updateKotsadmDeployment(existingDeployment, deployOptions); err != nil {
		return errors.Wrap(err, "failed to merge deployments")
	}

	_, err = clientset.AppsV1().Deployments(deployOptions.Namespace).Update(existingDeployment)
	if err != nil {
		return errors.Wrap(err, "failed to update kotsadm deployment")
	}

	return nil
}

func ensureKotsadmService(namespace string, clientset *kubernetes.Clientset) error {
	_, err := clientset.CoreV1().Services(namespace).Get("kotsadm", metav1.GetOptions{})
	if err != nil {
		if !kuberneteserrors.IsNotFound(err) {
			return errors.Wrap(err, "failed to get existing service")
		}

		_, err := clientset.CoreV1().Services(namespace).Create(kotsadmService(namespace))
		if err != nil {
			return errors.Wrap(err, "Failed to create service")
		}
	}

	return nil
}

// isKotsadmClusterScoped determines if the kotsadm pod should be running
// with cluster-wide permissions or not
func isKotsadmClusterScoped(applicationMetadata []byte) (bool, error) {
	if applicationMetadata == nil {
		return true, nil
	}

	decode := scheme.Codecs.UniversalDeserializer().Decode
	obj, gvk, err := decode(applicationMetadata, nil, nil)
	if err != nil {
		return false, errors.Wrap(err, "failed to decode application metadata")
	}

	if gvk.Group != "kots.io" || gvk.Version != "v1beta1" || gvk.Kind != "Application" {
		return false, errors.New("application metadata contained unepxected gvk")
	}

	application := obj.(*kotsv1beta1.Application)

	// An application can request cluster scope privileges quite simply
	if !application.Spec.RequireMinimalRBACPrivileges {
		return true, nil
	}

	return false, nil
}
