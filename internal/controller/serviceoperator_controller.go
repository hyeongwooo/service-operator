/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"strings"

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

// ServiceOperatorReconciler reconciles a ServiceOperator object
type ServiceOperatorReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=operator.wnguddn777.com,resources=serviceoperators,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=operator.wnguddn777.com,resources=serviceoperators/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=operator.wnguddn777.com,resources=serviceoperators/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete

// Reconcile reconciles a ServiceOperator object
func (r *ServiceOperatorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Retrieve the custom resource
	customResource := &operatorv1alpha1.ServiceOperator{}
	err := r.Client.Get(context.Background(), req.NamespacedName, customResource)
	if err != nil {
		if errors.IsNotFound(err) {
			// Custom resource not found, delete associated resources
			log.Info(fmt.Sprintf(`Custom resource for service "%s" does not exist, deleting associated resources`, req.Name))

			// Delete resources
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

	// Create or update Deployment
	err = r.Client.Create(ctx, newDeployment(req.Name, req.Namespace, customResource.Spec.Image, customResource.Spec.ContainerPort,
		customResource.Spec.Replicas, customResource.Spec.Resources))
	if err != nil {
		if errors.IsAlreadyExists(err) {
			log.Info(fmt.Sprintf(`Deployment for service "%s" already exists`, customResource.Name))
			deploymentNamespacedName := types.NamespacedName{
				Name:      customResource.Name,
				Namespace: customResource.Namespace,
			}

			deployment := appsv1.Deployment{}
			r.Client.Get(ctx, deploymentNamespacedName, &deployment)

			currentImage := deployment.Spec.Template.Spec.Containers[0].Image
			desiredImage := customResource.Spec.Image

			if currentImage != desiredImage {
				log.Info(fmt.Sprintf(`Image has updated from "%s" to "%s"`, currentImage, desiredImage))

				patch := client.StrategicMergeFrom(deployment.DeepCopy())
				deployment.Spec.Template.Spec.Containers[0].Image = desiredImage
				err := r.Client.Patch(ctx, &deployment, patch)
				if err != nil {
					log.Error(err, fmt.Sprintf(`Failed to update deployment "%s"`, customResource.Name))
					return ctrl.Result{}, err
				}
			}
		} else {
			log.Error(err, fmt.Sprintf(`Failed to create deployment "%s"`, customResource.Name))
			return ctrl.Result{}, err
		}
	}

	// Create or update Service
	err = r.Client.Create(ctx, newService(req.Name, req.Namespace, customResource.Spec.ServiceType, customResource.Spec.NodePort, customResource.Spec.Port))
	if err != nil {
		if errors.IsInvalid(err) && strings.Contains(err.Error(), "provided port is already allocated") {
			log.Info(fmt.Sprintf(`Service "%s" already exists`, customResource.Name))
			// TODO: handle service updates gracefully
		} else {
			log.Error(err, fmt.Sprintf(`Failed to create service "%s"`, customResource.Name))
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServiceOperatorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1alpha1.ServiceOperator{}).
		Complete(r)
}

// setResourceLabels sets labels for resources created by the operator
func setResourceLabels(name string) map[string]string {
	return map[string]string{
		"serviceoperator": name,
		"type":            "ServiceOperator",
	}
}

// newDeployment creates a new Deployment resource
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

// newService creates a new Service resource
func newService(name, namespace string, serviceType corev1.ServiceType, nodePort, port *int32) *corev1.Service {
	// Set default port if not specified
	if port == nil {
		defaultPort := int32(80)
		port = &defaultPort
	}

	// Initialize the ServicePort
	servicePort := corev1.ServicePort{
		Port: *port,
	}

	// Initialize the Service object
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

	// Set NodePort if the ServiceType is NodePort and NodePort is specified
	if serviceType == corev1.ServiceTypeNodePort {
		if nodePort != nil {
			service.Spec.Ports[0].NodePort = *nodePort
		}
	}

	return service
}
