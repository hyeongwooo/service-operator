package controller

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	operatorv1alpha1 "github.com/wnguddn777.com/service-operator/api/v1alpha1"
)

type ServiceOperatorReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=operator.wnguddn777.com,resources=serviceoperators,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=operator.wnguddn777.com,resources=serviceoperators/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=operator.wnguddn777.com,resources=serviceoperators/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete

func (r *ServiceOperatorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	customResource := &operatorv1alpha1.ServiceOperator{}
	err := r.Client.Get(ctx, req.NamespacedName, customResource)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Info(fmt.Sprintf(`Custom resource for service "%s" does not exist, deleting associated resources`, req.Name))

			deployErr := r.Client.Delete(ctx, newDeployment(req.Name, req.Namespace, "n/a", nil, nil, corev1.ResourceRequirements{}))
			if deployErr != nil && !errors.IsNotFound(deployErr) {
				log.Error(deployErr, fmt.Sprintf(`Failed to delete deployment "%s"`, req.Name))
			}

			serviceErr := r.Client.Delete(ctx, newService(req.Name, req.Namespace, corev1.ServiceTypeClusterIP, nil, nil))
			if serviceErr != nil && !errors.IsNotFound(serviceErr) {
				log.Error(serviceErr, fmt.Sprintf(`Failed to delete service "%s"`, req.Name))
			}

			if deployErr != nil || serviceErr != nil {
				return ctrl.Result{}, fmt.Errorf("%v\n%v", deployErr, serviceErr)
			}

			return ctrl.Result{}, nil
		} else {
			log.Error(err, fmt.Sprintf(`Failed to retrieve custom resource "%s"`, req.Name))
			return ctrl.Result{}, err
		}
	}

	deployment := &appsv1.Deployment{}
	deploymentNamespacedName := types.NamespacedName{
		Name:      customResource.Name,
		Namespace: customResource.Namespace,
	}

	err = r.Client.Get(ctx, deploymentNamespacedName, deployment)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Info(fmt.Sprintf(`Deployment for service "%s" does not exist, creating new one`, customResource.Name))
			err = r.Client.Create(ctx, newDeployment(req.Name, req.Namespace, customResource.Spec.Image, customResource.Spec.ContainerPort, customResource.Spec.Replicas, customResource.Spec.Resources))
			if err != nil {
				log.Error(err, fmt.Sprintf(`Failed to create deployment "%s"`, customResource.Name))
				return ctrl.Result{}, err
			}
		} else {
			log.Error(err, fmt.Sprintf(`Failed to get deployment "%s"`, customResource.Name))
			return ctrl.Result{}, err
		}
	} else {
		// Check for changes and update the Deployment if necessary
		updateDeployment := false

		if deployment.Spec.Template.Spec.Containers[0].Image != customResource.Spec.Image {
			log.Info(fmt.Sprintf(`Deployment image changed from "%s" to "%s"`, deployment.Spec.Template.Spec.Containers[0].Image, customResource.Spec.Image))
			deployment.Spec.Template.Spec.Containers[0].Image = customResource.Spec.Image
			updateDeployment = true
		}

		if *deployment.Spec.Replicas != *customResource.Spec.Replicas {
			log.Info(fmt.Sprintf(`Deployment replicas changed from "%d" to "%d"`, *deployment.Spec.Replicas, *customResource.Spec.Replicas))
			deployment.Spec.Replicas = customResource.Spec.Replicas
			updateDeployment = true
		}

		if updateDeployment {
			err = r.Client.Update(ctx, deployment)
			if err != nil {
				log.Error(err, fmt.Sprintf(`Failed to update deployment "%s"`, customResource.Name))
				return ctrl.Result{}, err
			}
		}
	}

	// Determine the service type
	// serviceType := customResource.Spec.ServiceType
	// if serviceType == "" {
	// 	serviceType = corev1.ServiceTypeClusterIP
	// }

	service := &corev1.Service{}
	serviceNamespacedName := types.NamespacedName{
		Name:      customResource.Name,
		Namespace: customResource.Namespace,
	}

	err = r.Client.Get(ctx, serviceNamespacedName, service)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Info(fmt.Sprintf(`Service for "%s" does not exist, creating new one`, customResource.Name))
			err = r.Client.Create(ctx, newService(req.Name, req.Namespace, customResource.Spec.ServiceType,
				customResource.Spec.NodePort, customResource.Spec.Port))
			if err != nil {
				log.Error(err, fmt.Sprintf(`Failed to create service "%s"`, customResource.Name))
				return ctrl.Result{}, err
			}
		} else {
			log.Error(err, fmt.Sprintf(`Failed to get service "%s"`, customResource.Name))
			return ctrl.Result{}, err
		}
	} else {
		// Check for changes and update the Service if necessary
		updateService := false

		if service.Spec.Type != customResource.Spec.ServiceType {
			log.Info(fmt.Sprintf(`Service type changed from "%s" to "%s"`, service.Spec.Type, customResource.Spec.ServiceType))
			service.Spec.Type = customResource.Spec.ServiceType
			updateService = true
		}

		if len(service.Spec.Ports) > 0 && service.Spec.Ports[0].Port != *customResource.Spec.Port {
			log.Info(fmt.Sprintf(`Service port changed from "%d" to "%d"`, service.Spec.Ports[0].Port, *customResource.Spec.Port))
			service.Spec.Ports[0].Port = *customResource.Spec.Port
			updateService = true
		}

		if service.Spec.Type == corev1.ServiceTypeNodePort && customResource.Spec.NodePort != nil && service.Spec.Ports[0].NodePort != *customResource.Spec.NodePort {
			log.Info(fmt.Sprintf(`Service NodePort changed from "%d" to "%d"`, service.Spec.Ports[0].NodePort, *customResource.Spec.NodePort))
			service.Spec.Ports[0].NodePort = *customResource.Spec.NodePort
			updateService = true
		}

		if updateService {
			err = r.Client.Update(ctx, service)
			if err != nil {
				log.Error(err, fmt.Sprintf(`Failed to update service "%s"`, customResource.Name))
				return ctrl.Result{}, err
			}
		}
	}

	return ctrl.Result{}, nil
}

func (r *ServiceOperatorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1alpha1.ServiceOperator{}).
		Complete(r)
}

func setResourceLabels(name string) map[string]string {
	return map[string]string{
		"serviceoperator": name,
		"type":            "ServiceOperator",
	}
}

func newDeployment(name, namespace, image string, containerPort *int32, replicas *int32, resources corev1.ResourceRequirements) *appsv1.Deployment {
	defaultReplicas := int32(2)
	if replicas == nil {
		replicas = &defaultReplicas
	}

	if containerPort == nil {
		defaultContainerPort := int32(80)
		containerPort = &defaultContainerPort
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    setResourceLabels(name),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: replicas,
			Selector: &metav1.LabelSelector{MatchLabels: setResourceLabels(name)},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: setResourceLabels(name)},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: image,
							Ports: []corev1.ContainerPort{{
								ContainerPort: *containerPort,
							}},
							Resources: resources,
						},
					},
				},
			},
		},
	}
}

func newService(name, namespace string, serviceType corev1.ServiceType, nodePort, port *int32) *corev1.Service {
	if port == nil {
		defaultPort := int32(80)
		port = &defaultPort
	}

	servicePort := corev1.ServicePort{
		Port: *port,
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    setResourceLabels(name),
		},
		Spec: corev1.ServiceSpec{
			Selector: setResourceLabels(name),
			Ports:    []corev1.ServicePort{servicePort},
			Type:     serviceType,
		},
	}

	if serviceType == corev1.ServiceTypeNodePort && nodePort != nil {
		service.Spec.Ports[0].NodePort = *nodePort
	}

	return service
}
