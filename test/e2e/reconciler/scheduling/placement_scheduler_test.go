/*
Copyright 2022 The KCP Authors.

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

package cluster

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	apisv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/apis/v1alpha1"
	schedulingv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/scheduling/v1alpha1"
	"github.com/kcp-dev/kcp/pkg/apis/third_party/conditions/util/conditions"
	workloadv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/workload/v1alpha1"
	kcpclient "github.com/kcp-dev/kcp/pkg/client/clientset/versioned"
	kubefixtures "github.com/kcp-dev/kcp/test/e2e/fixtures/kube"
	"github.com/kcp-dev/kcp/test/e2e/framework"
)

func TestPlacementUpdate(t *testing.T) {
	t.Parallel()

	ctx, cancelFunc := context.WithCancel(context.Background())
	t.Cleanup(cancelFunc)

	source := framework.SharedKcpServer(t)

	orgClusterName := framework.NewOrganizationFixture(t, source)
	locationClusterName := framework.NewWorkspaceFixture(t, source, orgClusterName)
	userClusterName := framework.NewWorkspaceFixture(t, source, orgClusterName)

	kubeClusterClient, err := kubernetes.NewClusterForConfig(source.DefaultConfig(t))
	require.NoError(t, err)
	kcpClusterClient, err := kcpclient.NewClusterForConfig(source.DefaultConfig(t))
	require.NoError(t, err)

	t.Logf("Check that there is no services resource in the user workspace")
	_, err = kubeClusterClient.Cluster(userClusterName).CoreV1().Services("").List(ctx, metav1.ListOptions{})
	require.Error(t, err)

	firstSyncTargetName := fmt.Sprintf("synctarget-%d", +rand.Intn(1000000))
	t.Logf("Creating a SyncTarget and syncer in %s", locationClusterName)
	syncerFixture := framework.SyncerFixture{
		ResourcesToSync:      sets.NewString("services"),
		UpstreamServer:       source,
		WorkspaceClusterName: locationClusterName,
		SyncTargetName:       firstSyncTargetName,
		InstallCRDs: func(config *rest.Config, isLogicalCluster bool) {
			if !isLogicalCluster {
				// Only need to install services and ingresses in a logical cluster
				return
			}
			sinkCrdClient, err := apiextensionsclientset.NewForConfig(config)
			require.NoError(t, err, "failed to create apiextensions client")
			t.Logf("Installing test CRDs into sink cluster...")
			kubefixtures.Create(t, sinkCrdClient.ApiextensionsV1().CustomResourceDefinitions(),
				metav1.GroupResource{Group: "core.k8s.io", Resource: "services"},
			)
			require.NoError(t, err)
		},
	}.Start(t)

	t.Log("Wait for \"default\" location")
	require.Eventually(t, func() bool {
		_, err = kcpClusterClient.Cluster(locationClusterName).SchedulingV1alpha1().Locations().Get(ctx, "default", metav1.GetOptions{})
		return err == nil
	}, wait.ForeverTestTimeout, time.Millisecond*100)

	binding := &apisv1alpha1.APIBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "kubernetes",
		},
		Spec: apisv1alpha1.APIBindingSpec{
			Reference: apisv1alpha1.ExportReference{
				Workspace: &apisv1alpha1.WorkspaceExportReference{
					Path:       locationClusterName.String(),
					ExportName: "kubernetes",
				},
			},
		},
	}

	t.Logf("Create a binding in the user workspace")
	_, err = kcpClusterClient.Cluster(userClusterName).ApisV1alpha1().APIBindings().Create(ctx, binding, metav1.CreateOptions{})
	require.NoError(t, err)

	t.Logf("Wait for binding to be ready")
	framework.Eventually(t, func() (bool, string) {
		binding, err := kcpClusterClient.Cluster(userClusterName).ApisV1alpha1().APIBindings().Get(ctx, binding.Name, metav1.GetOptions{})
		require.NoError(t, err)

		return conditions.IsTrue(binding, apisv1alpha1.InitialBindingCompleted), fmt.Sprintf("binding not bound: %s", toYaml(binding))
	}, wait.ForeverTestTimeout, time.Millisecond*100)

	t.Logf("Wait for placement to be ready")
	framework.Eventually(t, func() (bool, string) {
		placement, err := kcpClusterClient.Cluster(userClusterName).SchedulingV1alpha1().Placements().Get(ctx, "default", metav1.GetOptions{})
		require.NoError(t, err)

		return conditions.IsTrue(placement, schedulingv1alpha1.PlacementReady), fmt.Sprintf("placement is not ready: %s", toYaml(binding))
	}, wait.ForeverTestTimeout, time.Millisecond*100)

	t.Logf("Wait for being able to list Services in the user workspace")
	require.Eventually(t, func() bool {
		_, err := kubeClusterClient.Cluster(userClusterName).CoreV1().Services("").List(ctx, metav1.ListOptions{})
		if errors.IsNotFound(err) {
			return false
		} else if err != nil {
			klog.Errorf("Failed to list Services: %v", err)
			return false
		}
		return true
	}, wait.ForeverTestTimeout, time.Millisecond*100)

	t.Logf("Create a service in the user workspace")
	_, err = kubeClusterClient.Cluster(userClusterName).CoreV1().Services("default").Create(ctx, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "first",
			Labels: map[string]string{
				"test.workload.kcp.dev": firstSyncTargetName,
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port:     80,
					Protocol: corev1.ProtocolTCP,
				},
			},
		},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	t.Logf("Wait for the service to have the sync label")
	framework.Eventually(t, func() (bool, string) {
		svc, err := kubeClusterClient.Cluster(userClusterName).CoreV1().Services("default").Get(ctx, "first", metav1.GetOptions{})
		if err != nil {
			return false, fmt.Sprintf("Failed to get service: %v", err)
		}

		return svc.Labels[workloadv1alpha1.ClusterResourceStateLabelPrefix+firstSyncTargetName] == string(workloadv1alpha1.ResourceStateSync), ""
	}, wait.ForeverTestTimeout, time.Millisecond*100)

	t.Logf("Wait for the service to be sync to the downstream cluster")
	var downstreamServices *corev1.ServiceList
	framework.Eventually(t, func() (bool, string) {
		downstreamServices, err = syncerFixture.DownstreamKubeClient.CoreV1().Services("").List(ctx, metav1.ListOptions{
			LabelSelector: "test.workload.kcp.dev=" + firstSyncTargetName,
		})

		if err != nil {
			return false, fmt.Sprintf("Failed to list service: %v", err)
		}

		if len(downstreamServices.Items) < 1 {
			return false, "service is not synced"
		}
		return true, ""
	}, wait.ForeverTestTimeout, time.Millisecond*100)

	t.Logf("Update placement to disable scheduling on the ns")
	framework.Eventually(t, func() (bool, string) {
		placement, err := kcpClusterClient.Cluster(userClusterName).SchedulingV1alpha1().Placements().Get(ctx, "default", metav1.GetOptions{})
		if err != nil {
			return false, fmt.Sprintf("Failed to get placement: %v", err)
		}

		placement.Spec.NamespaceSelector = nil
		_, err = kcpClusterClient.Cluster(userClusterName).SchedulingV1alpha1().Placements().Update(ctx, placement, metav1.UpdateOptions{})
		if err != nil {
			return false, fmt.Sprintf("Failed to update placement: %v", err)
		}

		return true, ""
	}, wait.ForeverTestTimeout, time.Millisecond*100)

	t.Logf("Placement should turn to unbound phase")
	framework.Eventually(t, func() (bool, string) {
		placement, err := kcpClusterClient.Cluster(userClusterName).SchedulingV1alpha1().Placements().Get(ctx, "default", metav1.GetOptions{})
		if err != nil {
			return false, fmt.Sprintf("Failed to get placement: %v", err)
		}

		return placement.Status.Phase == schedulingv1alpha1.PlacementUnbound, ""
	}, wait.ForeverTestTimeout, time.Millisecond*100)

	framework.Eventually(t, func() (bool, string) {
		ns, err := kubeClusterClient.Cluster(userClusterName).CoreV1().Namespaces().Get(ctx, "default", metav1.GetOptions{})
		if err != nil {
			return false, fmt.Sprintf("Failed to get ns: %v", err)
		}

		if len(ns.Annotations[workloadv1alpha1.InternalClusterDeletionTimestampAnnotationPrefix+firstSyncTargetName]) == 0 {
			return false, fmt.Sprintf("resource should be removed but got %s", toYaml(ns))
		}
		return true, ""
	}, wait.ForeverTestTimeout, time.Millisecond*100)

	framework.Eventually(t, func() (bool, string) {
		svc, err := kubeClusterClient.Cluster(userClusterName).CoreV1().Services("default").Get(ctx, "first", metav1.GetOptions{})
		if err != nil {
			return false, fmt.Sprintf("Failed to get service: %v", err)
		}

		if len(svc.Annotations[workloadv1alpha1.InternalClusterDeletionTimestampAnnotationPrefix+firstSyncTargetName]) == 0 {
			return false, fmt.Sprintf("resource should be removed but got %s", toYaml(svc))
		}
		return true, ""
	}, wait.ForeverTestTimeout, time.Millisecond*100)

	t.Logf("Wait for the service to be removed in the downstream cluster")
	require.Eventually(t, func() bool {
		downstreamServices, err = syncerFixture.DownstreamKubeClient.CoreV1().Services("").List(ctx, metav1.ListOptions{
			LabelSelector: "test.workload.kcp.dev=" + firstSyncTargetName,
		})
		if errors.IsNotFound(err) {
			return false
		} else if err != nil {
			klog.Errorf("Failed to list Services: %v", err)
			return false
		} else if len(downstreamServices.Items) != 0 {
			return false
		}
		return true
	}, wait.ForeverTestTimeout, time.Millisecond*100)

	framework.Eventually(t, func() (bool, string) {
		placement, err := kcpClusterClient.Cluster(userClusterName).SchedulingV1alpha1().Placements().Get(ctx, "default", metav1.GetOptions{})
		if err != nil {
			return false, fmt.Sprintf("Failed to get placement: %v", err)
		}

		placement.Spec.LocationSelectors = []metav1.LabelSelector{
			{
				MatchLabels: map[string]string{
					"foo": "bar",
				},
			},
		}
		_, err = kcpClusterClient.Cluster(userClusterName).SchedulingV1alpha1().Placements().Update(ctx, placement, metav1.UpdateOptions{})
		if err != nil {
			return false, fmt.Sprintf("Failed to update placement: %v", err)
		}
		return true, ""
	}, wait.ForeverTestTimeout, time.Millisecond*100)

	t.Logf("Placement should turn to pending phase")
	framework.Eventually(t, func() (bool, string) {
		placement, err := kcpClusterClient.Cluster(userClusterName).SchedulingV1alpha1().Placements().Get(ctx, "default", metav1.GetOptions{})
		if err != nil {
			return false, fmt.Sprintf("Failed to get placement: %v", err)
		}

		return placement.Status.Phase == schedulingv1alpha1.PlacementPending, ""
	}, wait.ForeverTestTimeout, time.Millisecond*100)

	t.Logf("Create a new placement to include the location")
	newPlacement := &schedulingv1alpha1.Placement{
		ObjectMeta: metav1.ObjectMeta{
			Name: "new-placement",
		},
		Spec: schedulingv1alpha1.PlacementSpec{
			LocationSelectors: []metav1.LabelSelector{{}},
			NamespaceSelector: &metav1.LabelSelector{},
			LocationResource: schedulingv1alpha1.GroupVersionResource{
				Group:    "workload.kcp.dev",
				Version:  "v1alpha1",
				Resource: "synctargets",
			},
			LocationWorkspace: locationClusterName.String(),
		},
	}
	_, err = kcpClusterClient.Cluster(userClusterName).SchedulingV1alpha1().Placements().Create(ctx, newPlacement, metav1.CreateOptions{})
	require.NoError(t, err)

	t.Logf("Wait for new placement to be ready")
	framework.Eventually(t, func() (bool, string) {
		placement, err := kcpClusterClient.Cluster(userClusterName).SchedulingV1alpha1().Placements().Get(ctx, newPlacement.Name, metav1.GetOptions{})
		if err != nil {
			return false, fmt.Sprintf("Failed to get placement: %v", err)
		}

		return conditions.IsTrue(placement, schedulingv1alpha1.PlacementReady), fmt.Sprintf("placement is not ready: %s", toYaml(binding))
	}, wait.ForeverTestTimeout, time.Millisecond*100)

	t.Logf("Wait for resource to by synced again")
	framework.Eventually(t, func() (bool, string) {
		svc, err := kubeClusterClient.Cluster(userClusterName).CoreV1().Services("default").Get(ctx, "first", metav1.GetOptions{})
		if err != nil {
			return false, fmt.Sprintf("Failed to get service: %v", err)
		}

		if len(svc.Annotations[workloadv1alpha1.InternalClusterDeletionTimestampAnnotationPrefix+firstSyncTargetName]) != 0 {
			return false, fmt.Sprintf("resource should not be removed but got %s", toYaml(svc))
		}
		return svc.Labels[workloadv1alpha1.ClusterResourceStateLabelPrefix+firstSyncTargetName] == string(workloadv1alpha1.ResourceStateSync), ""
	}, wait.ForeverTestTimeout, time.Millisecond*100)

	t.Logf("Wait for the service to be sync to the downstream cluster")
	require.Eventually(t, func() bool {
		downstreamServices, err = syncerFixture.DownstreamKubeClient.CoreV1().Services("").List(ctx, metav1.ListOptions{
			LabelSelector: "test.workload.kcp.dev=" + firstSyncTargetName,
		})
		if errors.IsNotFound(err) {
			return false
		} else if err != nil {
			klog.Errorf("Failed to list Services: %v", err)
			return false
		} else if len(downstreamServices.Items) < 1 {
			return false
		}
		return true
	}, wait.ForeverTestTimeout, time.Millisecond*100)
}
