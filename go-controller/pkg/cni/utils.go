package cni

import (
	"time"

	"github.com/sirupsen/logrus"

	"k8s.io/apimachinery/pkg/util/wait"
	
	"github.com/ovn-org/ovn-kubernetes/go-controller/pkg/config"
	"github.com/ovn-org/ovn-kubernetes/go-controller/pkg/kube"
	"github.com/ovn-org/ovn-kubernetes/go-controller/pkg/util"
)

func GetPodInfoSmartNIC(namespace string, podName string) error {
	envConfig := config.KubernetesConfig{
                APIServer:  "https://10.212.0.220:6443",
                Kubeconfig:  "/etc/kubernetes/admin.conf",
        }

        clientset, err := util.NewClientset(&envConfig)
	if err != nil {
		logrus.Errorf("Could not create clientset for kubernetes: %v", err)
		return err
	}
	kubecli := &kube.Kube{KClient: clientset}
	kubecli.GetAnnotationsOnPod(namespace, podName)
	return nil
}


func SetPodInfoSmartNic(namespace, podName string) error{
	logrus.Infof("Moshe!!! SetPodInfoSmartNic")
	envConfig := config.KubernetesConfig{
                APIServer:  "https://10.212.0.220:6443",
                Kubeconfig:  "/etc/kubernetes/admin.conf",
	}
	logrus.Infof("Moshe!!! clientset for kubernetes: %v", envConfig)
        clientset, err := util.NewClientset(&envConfig)
	if err != nil {
		logrus.Errorf("Could not create clientset for kubernetes: %v", err)
		return err
	}
	kubecli := &kube.Kube{KClient: clientset}
	//TODO add container id in anotation
	// TOOD Error check
	kubecli.SetAnnotationOnPodString(namespace, podName, "ovn.smartnic.pf", "0")
	kubecli.SetAnnotationOnPodString(namespace, podName, "ovn.smartnic.vf", "0")
	return nil
}

func GetPodInfo(namespace string, podName string) (podInfo *util.PodAnnotation, annotation map[string]string) {
	envConfig := config.KubernetesConfig{
                APIServer:  "https://10.212.0.220:6443",
                Kubeconfig:  "/etc/kubernetes/admin.conf",
	}
	clientset, err := util.NewClientset(&envConfig)
	if err != nil {
		logrus.Errorf("Could not create clientset for kubernetes: %v", err)
		return nil, nil
	}
	kubecli := &kube.Kube{KClient: clientset}

	// Get the IP address and MAC address from the API server.
	// Exponential back off ~32 seconds + 7* t(api call)
	var annotationBackoff = wait.Backoff{Duration: 1 * time.Second, Steps: 7, Factor: 1.5, Jitter: 0.1}
	//var annotation map[string]string
	if err = wait.ExponentialBackoff(annotationBackoff, func() (bool, error) {
		annotation, err = kubecli.GetAnnotationsOnPod(namespace, podName)
		if err != nil {
			// TODO: check if err is non recoverable
			logrus.Warningf("Error while obtaining pod annotations - %v", err)
			return false, nil
		}
		if _, ok := annotation["ovn"]; ok {
			return true, nil
		}
		return false, nil
	}); err != nil {
		logrus.Errorf("failed to get pod annotation - %v", err)
		return nil, nil
	}

	ovnAnnotation, ok := annotation["ovn"]
	if !ok {
		logrus.Errorf("failed to get ovn annotation from pod")
		return nil, nil
	}

	podInfo, err = util.UnmarshalPodAnnotation(ovnAnnotation)
	if err != nil {
		logrus.Errorf("unmarshal ovn annotation failed: %v", err)
		return nil, nil
	}

	return podInfo, annotation
}
