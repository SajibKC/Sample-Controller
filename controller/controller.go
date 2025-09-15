package controller

import (
	"context"
	"fmt"
	"time"

	samplecontrollerv1alpha1 "github.com/example-inc/sample-controller/pkg/apis/samplecontroller/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	typedappsv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

const controllerAgentName = "sample-controller"

// Controller is the controller implementation for CronTab resources
type Controller struct {
	kubeclientset kubernetes.Interface

	deploymentsClient typedappsv1.DeploymentInterface
	servicesClient   typedcorev1.ServiceInterface

	cronTabInformer cache.SharedIndexInformer
	workqueue       workqueue.RateLimitingInterface
}

// NewController returns a new sample controller
func NewController(
	kubeclientset kubernetes.Interface,
	cronTabInformer cache.SharedIndexInformer) *Controller {

	deploymentsClient := kubeclientset.AppsV1().Deployments(corev1.NamespaceDefault)
	servicesClient := kubeclientset.CoreV1().Services(corev1.NamespaceDefault)

	controller := &Controller{
		kubeclientset:    kubeclientset,
		deploymentsClient: deploymentsClient,
		servicesClient:   servicesClient,
		cronTabInformer: cronTabInformer,
		workqueue:       workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "CronTabs"),
	}

	klog.Info("Setting up event handlers")
	cronTabInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueCronTab,
		UpdateFunc: func(old, new interface{}) {
			controller.enqueueCronTab(new)
		},
		DeleteFunc: controller.enqueueCronTab,
	})

	return controller
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	defer c.workqueue.ShutDown()

	klog.Info("Starting CronTab controller")

	klog.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.cronTabInformer.HasSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	klog.Info("Starting workers")
	for i := 0; i < threadiness; i++ {
		go func() {
			for c.processNextWorkItem() {
			}
		}()
	}

	klog.Info("Started workers")
	<-stopCh
	klog.Info("Shutting down workers")

	return nil
}

func (c *Controller) processNextWorkItem() bool {
	obj, shutdown := c.workqueue.Get()

	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.workqueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.workqueue.Forget(obj)
			klog.Errorf("expected string in workqueue but got %#v", obj)
			return nil
		}
		if err := c.syncHandler(key); err != nil {
			c.workqueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.workqueue.Forget(obj)
		klog.Infof("Successfully synced '%s'", key)
		return nil
	}(obj)

	if err != nil {
		klog.Error(err)
		return true
	}

	return true
}

func (c *Controller) enqueueCronTab(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		klog.Errorf("Error creating key for object: %v", err)
		return
	}
	c.workqueue.Add(key)
}

func (c *Controller) syncHandler(key string) error {
	obj, exists, err := c.cronTabInformer.GetIndexer().GetByKey(key)
	if err != nil {
		klog.Errorf("Fetching object with key %s from store failed: %v", key, err)
		return err
	}

	if !exists {
		klog.Infof("CronTab %s does not exist anymore\n", key)
		return nil
	}

	cronTab, ok := obj.(*samplecontrollerv1alpha1.CronTab)
	if !ok {
		klog.Errorf("Expected CronTab but got %#v", obj)
		return nil
	}

	klog.Infof("Syncing CronTab: %s", cronTab.Name)

	deploymentName := cronTab.Name + "-deployment"
	serviceName := cronTab.Name + "-service"

	deployment, err := c.deploymentsClient.Get(context.TODO(), deploymentName, metav1.GetOptions{})
	if err != nil {
		klog.Infof("Creating deployment %s for CronTab %s", deploymentName, cronTab.Name)
		deployment = c.newDeployment(cronTab)
		_, err = c.deploymentsClient.Create(context.TODO(), deployment, metav1.CreateOptions{})
		if err != nil {
			return err
		}
	} else {
		if *deployment.Spec.Replicas != cronTab.Spec.Replicas || deployment.Spec.Template.Spec.Containers[0].Image != cronTab.Spec.Image {
			klog.Infof("Updating deployment %s for CronTab %s", deploymentName, cronTab.Name)
			deployment.Spec.Replicas = &cronTab.Spec.Replicas
			deployment.Spec.Template.Spec.Containers[0].Image = cronTab.Spec.Image
			_, err = c.deploymentsClient.Update(context.TODO(), deployment, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
		}
	}

	service, err := c.servicesClient.Get(context.TODO(), serviceName, metav1.GetOptions{})
	if err != nil {
		klog.Infof("Creating service %s for CronTab %s", serviceName, cronTab.Name)
		service = c.newService(cronTab)
		_, err = c.servicesClient.Create(context.TODO(), service, metav1.CreateOptions{})
		if err != nil {
			return err
		}
	}

	err = c.updateCronTabStatus(cronTab, deployment)
	if err != nil {
		return err
	}

	return nil
}

func (c *Controller) newDeployment(cronTab *samplecontrollerv1alpha1.CronTab) *appsv1.Deployment {
	labels := map[string]string{
		"app": cronTab.Name,
	}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cronTab.Name + "-deployment",
			Namespace: corev1.NamespaceDefault,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(cronTab, samplecontrollerv1alpha1.SchemeGroupVersion.WithKind("CronTab")),
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &cronTab.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "cronjob",
							Image: cronTab.Spec.Image,
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									Protocol:      corev1.ProtocolTCP,
									ContainerPort: 80,
								},
							},
						},
					},
				},
			},
		},
	}
}

func (c *Controller) newService(cronTab *samplecontrollerv1alpha1.CronTab) *corev1.Service {
	labels := map[string]string{
		"app": cronTab.Name,
	}
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cronTab.Name + "-service",
			Namespace: corev1.NamespaceDefault,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(cronTab, samplecontrollerv1alpha1.SchemeGroupVersion.WithKind("CronTab")),
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports: []corev1.ServicePort{
				{
					Protocol:   corev1.ProtocolTCP,
					Port:       80,
					TargetPort: intstr.FromInt(80),
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}
}

func (c *Controller) updateCronTabStatus(cronTab *samplecontrollerv1alpha1.CronTab, deployment *appsv1.Deployment) error {
	availableReplicas := deployment.Status.AvailableReplicas
	if cronTab.Status.AvailableReplicas != availableReplicas {
		cronTabCopy := cronTab.DeepCopy()
		cronTabCopy.Status.AvailableReplicas = availableReplicas
		_, err := c.kubeclientset.RESTClient().
			Put().
			Namespace(cronTab.Namespace).
			Resource("crontabs").
			Name(cronTab.Name).
			Body(cronTabCopy).
			Do(context.TODO()).
			Get()
		if err != nil {
			return err
		}
		klog.Infof("Updated CronTab %s status to %d available replicas", cronTab.Name, availableReplicas)
	}
	return nil
}

func int32Ptr(i int32) *int32 { return &i }
func int64Ptr(i int64) *int64 { return &i }
func boolPtr(b bool) *bool    { return &b }

func (c *Controller) waitForDeploymentReady(deploymentName string, timeout time.Duration) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	timeoutCh := time.After(timeout)

	for {
		select {
		case <-ticker.C:
			deployment, err := c.deploymentsClient.Get(context.TODO(), deploymentName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if deployment.Status.ReadyReplicas == *deployment.Spec.Replicas {
				return nil
			}
		case <-timeoutCh:
			return fmt.Errorf("timed out waiting for deployment %s to be ready", deploymentName)
		}
	}
}
func (c *Controller) waitForServiceReady(serviceName string, timeout time.Duration) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	timeoutCh := time.After(timeout)

	for {
		select {
		case <-ticker.C:
			_, err := c.servicesClient.Get(context.TODO(), serviceName, metav1.GetOptions{})
			if err == nil {
				return nil
			}
		case <-timeoutCh:
			return fmt.Errorf("timed out waiting for service %s to be ready", serviceName)
		}
	}
}