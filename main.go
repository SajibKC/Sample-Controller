package main

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
) 

func main() {
	config, _ := rest.InClusterConfig()
	clientset, _ := kubernetes.NewForConfig(config)
	for {
		replicas := int32(2)
		image := "nginx:latest"

		deploy := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "new_sajib", Namespace: "default"},
			Spec: appsv1.DeploymentSpec{
				Replicas: &replicas,
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "sajib"}},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "sajib"}},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name:  "nginx",
							Image: image,
						}},
					},
				},
			},
		}

		// Create or Update Deployment
		_, err := clientset.AppsV1().Deployments("default").Create(context.TODO(), deploy, metav1.CreateOptions{})
		if err != nil {
			fmt.Println("Deployment might already exist:", err)
		} else {
			fmt.Println("Deployment created for Sajib")
		}

		time.Sleep(30 * time.Second) // fake reconcile loop
	}
}
