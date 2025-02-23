// Copyright 2022 Authors of spidernet-io
// SPDX-License-Identifier: Apache-2.0
package labelselector_test

import (
	"context"
	"encoding/json"
	"time"

	corev1 "k8s.io/api/core/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spidernet-io/e2eframework/tools"
	"github.com/spidernet-io/spiderpool/pkg/constant"
	spiderpoolv1 "github.com/spidernet-io/spiderpool/pkg/k8s/apis/v1"
	"github.com/spidernet-io/spiderpool/pkg/types"
	"github.com/spidernet-io/spiderpool/test/e2e/common"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("test selector", Label("labelselector"), func() {
	Context("test different selector", func() {
		var workerNodeLabel, controlNodeLabel map[string]string
		var (
			matchedPodName, matchedNamespace     string
			unmatchedPodName, unmatchedNamespace string
		)
		var (
			v4PoolName string
			v6PoolName string
			v4Pool     *spiderpoolv1.SpiderIPPool
			v6Pool     *spiderpoolv1.SpiderIPPool
		)

		BeforeEach(func() {
			// init namespace name and create
			matchedNamespace = "matched-ns-" + tools.RandomName()
			unmatchedNamespace = "unmatched-ns-" + tools.RandomName()

			for _, namespace := range []string{matchedNamespace, unmatchedNamespace} {
				GinkgoWriter.Printf("create namespace %v \n", namespace)
				err := frame.CreateNamespace(namespace)
				Expect(err).NotTo(HaveOccurred(), "failed to create namespace %v", namespace)
			}

			// init test pod name
			matchedPodName = "matched-pod-" + tools.RandomName()
			unmatchedPodName = "unmatched-pod-" + tools.RandomName()

			// get node list
			GinkgoWriter.Println("get node list")
			controlNodes := []corev1.Node{}
			workerNodes := []corev1.Node{}

			nodeList, err := frame.GetNodeList()
			Expect(err).NotTo(HaveOccurred())
			Expect(nodeList).NotTo(BeNil())

			for _, node := range nodeList.Items {
				if _, ok := node.GetLabels()["node-role.kubernetes.io/master"]; ok {
					controlNodes = append(controlNodes, node)
				} else {
					workerNodes = append(workerNodes, node)
				}
			}

			workerNode := workerNodes[0]
			controlNode := controlNodes[0]
			GinkgoWriter.Printf("worker node: %v, controller node: %v\n", workerNode.Name, controlNode.Name)

			workerNodeLabel = workerNode.GetLabels()
			controlNodeLabel = controlNode.GetLabels()
			GinkgoWriter.Printf("workerNodeLabel:%v, controlNodeLabel:%v\n", workerNodeLabel, controlNodeLabel)

			// set namespace label
			GinkgoWriter.Printf("label namespace %v\n", matchedNamespace)
			ns, err := frame.GetNamespace(matchedNamespace)
			Expect(ns).NotTo(BeNil())
			Expect(err).NotTo(HaveOccurred())
			ns.Labels = map[string]string{matchedNamespace: matchedNamespace}
			Expect(frame.UpdateResource(ns)).To(Succeed())

			if frame.Info.IpV4Enabled {
				// create v4 ippool
				v4PoolName, v4Pool = common.GenerateExampleIpv4poolObject(1)
				GinkgoWriter.Printf("create v4 ippool %v\n", v4PoolName)
				v4Pool.Spec.NodeAffinity = new(v1.LabelSelector)
				v4Pool.Spec.NamesapceAffinity = new(v1.LabelSelector)
				v4Pool.Spec.PodAffinity = new(v1.LabelSelector)
				v4Pool.Spec.NodeAffinity.MatchLabels = workerNodeLabel
				v4Pool.Spec.NamesapceAffinity.MatchLabels = ns.Labels
				v4Pool.Spec.PodAffinity.MatchLabels = map[string]string{matchedPodName: matchedPodName}
				createIPPool(v4Pool)
			}
			if frame.Info.IpV6Enabled {
				// create v6 ippool
				v6PoolName, v6Pool = common.GenerateExampleIpv6poolObject(1)
				GinkgoWriter.Printf("create v6 ippool %v\n", v6PoolName)
				v6Pool.Spec.NodeAffinity = new(v1.LabelSelector)
				v6Pool.Spec.NamesapceAffinity = new(v1.LabelSelector)
				v6Pool.Spec.PodAffinity = new(v1.LabelSelector)
				v6Pool.Spec.NodeAffinity.MatchLabels = workerNodeLabel
				v6Pool.Spec.NamesapceAffinity.MatchLabels = ns.Labels
				v6Pool.Spec.PodAffinity.MatchLabels = map[string]string{matchedPodName: matchedPodName}
				createIPPool(v6Pool)
			}

			DeferCleanup(func() {
				// delete namespace
				for _, namespace := range []string{matchedNamespace, unmatchedNamespace} {
					GinkgoWriter.Printf("delete namespace %v \n", namespace)
					err := frame.DeleteNamespace(namespace)
					Expect(err).NotTo(HaveOccurred(), "failed to delete namespace %v", namespace)
				}

				// delete ippool
				if frame.Info.IpV4Enabled {
					deleteIPPoolUntilFinish(v4PoolName)
				}
				if frame.Info.IpV6Enabled {
					deleteIPPoolUntilFinish(v6PoolName)
				}
			})
		})
		DescribeTable("create pod with ippool that matched different selector", func(isNodeMatched, isNamespaceMatched, isPodMatched bool) {
			var namespaceNM, podNM string
			var nodeLabel map[string]string
			allMatched := false

			if isNodeMatched && isNamespaceMatched && isPodMatched {
				allMatched = true
				namespaceNM = matchedNamespace
				podNM = matchedPodName
				nodeLabel = workerNodeLabel
			}
			if !isNodeMatched {
				namespaceNM = matchedNamespace
				podNM = matchedPodName
				nodeLabel = controlNodeLabel
			}
			if !isNamespaceMatched {
				namespaceNM = unmatchedNamespace
				podNM = matchedPodName
				nodeLabel = workerNodeLabel
			}
			if !isPodMatched {
				namespaceNM = matchedNamespace
				podNM = unmatchedPodName
				nodeLabel = workerNodeLabel
			}

			// create pod
			GinkgoWriter.Printf("create pod %v/%v\n", namespaceNM, podNM)
			podObj := common.GenerateExamplePodYaml(podNM, namespaceNM)
			Expect(podObj).NotTo(BeNil())
			podObj.Spec.NodeSelector = nodeLabel

			podAnno := types.AnnoPodIPPoolValue{}

			if frame.Info.IpV4Enabled {
				podAnno.IPv4Pools = []string{v4PoolName}
			}
			if frame.Info.IpV6Enabled {
				podAnno.IPv6Pools = []string{v6PoolName}
			}
			b, err := json.Marshal(podAnno)
			podAnnoStr := string(b)
			Expect(err).NotTo(HaveOccurred())

			podObj.Annotations = map[string]string{
				constant.AnnoPodIPPool: podAnnoStr,
			}
			GinkgoWriter.Printf("podObj: %v\n", podObj)

			if allMatched {
				GinkgoWriter.Println("when matched selector")
				Expect(frame.CreatePod(podObj)).To(Succeed())
				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				defer cancel()
				pod, err := frame.WaitPodStarted(podNM, namespaceNM, ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(pod).NotTo(BeNil())
			}
			if !allMatched {
				GinkgoWriter.Println("when unmatched selector")
				Expect(frame.CreatePod(podObj)).To(Succeed())
				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				defer cancel()
				Expect(frame.WaitExceptEventOccurred(ctx, common.PodEventKind, podNM, namespaceNM, common.GetIpamAllocationFailed)).To(Succeed())
				GinkgoWriter.Printf("succeeded to matched the message %v\n", common.GetIpamAllocationFailed)
			}
		},
			Entry("succeed to run pod who is bound to an ippool set with matched nodeSelector namespaceSelector and podSelector", Label("smoke", "L00001", "L00003", "L00005"), true, true, true),
			Entry("failed to run pod who is bound to an ippool set with no-matched nodeSelector", Label("L00002"), false, true, true),
			Entry("failed to run pod who is bound to an ippool set with no-matched namespaceSelector", Label("L00004"), true, false, true),
			Entry("failed to run pod who is bound to an ippool set with no-matched podSelector", Label("L00006"), true, true, false),
		)
	})

	Context("cross-zone daemonSet", func() {
		var namespace, daemonSetName string
		var err error

		nodeV4PoolMap := make(map[string][]string)
		nodeV6PoolMap := make(map[string][]string)
		allV4PoolNameList := make([]string, 0)
		allV6PoolNameList := make([]string, 0)

		BeforeEach(func() {
			// create namespace
			namespace = "ns" + tools.RandomName()
			GinkgoWriter.Printf("create namespace %v \n", namespace)
			err = frame.CreateNamespace(namespace)
			Expect(err).NotTo(HaveOccurred(), "failed to create namespace %v", namespace)
			GinkgoWriter.Printf("succeed to create namespace %v \n", namespace)

			// daemonSetName name
			daemonSetName = "daemonset" + tools.RandomName()

			// get node list
			GinkgoWriter.Println("get node list")
			nodeList, err := frame.GetNodeList()
			Expect(err).NotTo(HaveOccurred())
			Expect(nodeList).NotTo(BeNil())

			if len(nodeList.Items) < 2 {
				Skip("skip: this case need 2 nodes at least")
			}

			for _, node := range nodeList.Items {
				// create ippool
				if frame.Info.IpV4Enabled {
					v4PoolName, v4Pool := common.GenerateExampleIpv4poolObject(1)
					GinkgoWriter.Printf("create v4 ippool %v\n", v4PoolName)
					v4Pool.Spec.NodeAffinity = new(v1.LabelSelector)
					v4Pool.Spec.NodeAffinity.MatchLabels = node.Labels
					createIPPool(v4Pool)

					allV4PoolNameList = append(allV4PoolNameList, v4PoolName)
					nodeV4PoolMap[node.Name] = []string{v4PoolName}

					GinkgoWriter.Printf("node: %v, v4PoolNameList: %+v \n", node.Name, nodeV4PoolMap[node.Name])
				}
				if frame.Info.IpV6Enabled {
					// create v6 ippool
					v6PoolName, v6Pool := common.GenerateExampleIpv6poolObject(1)
					GinkgoWriter.Printf("create v6 ippool %v\n", v6PoolName)
					v6Pool.Spec.NodeAffinity = new(v1.LabelSelector)
					v6Pool.Spec.NodeAffinity.MatchLabels = node.Labels
					createIPPool(v6Pool)

					allV6PoolNameList = append(allV6PoolNameList, v6PoolName)
					nodeV6PoolMap[node.Name] = []string{v6PoolName}

					GinkgoWriter.Printf("node: %v, v6PoolNameList: %+v \n", node.Name, nodeV6PoolMap[node.Name])
				}
			}

			DeferCleanup(func() {
				// delete namespace
				GinkgoWriter.Printf("delete namespace %v \n", namespace)
				err := frame.DeleteNamespace(namespace)
				Expect(err).NotTo(HaveOccurred(), "failed to delete namespace %v", namespace)
				GinkgoWriter.Printf("succeed to delete namespace %v \n", namespace)

				// delete ippool
				if frame.Info.IpV4Enabled {
					for _, poolName := range allV4PoolNameList {
						deleteIPPoolUntilFinish(poolName)
					}
				}
				if frame.Info.IpV6Enabled {
					for _, poolName := range allV6PoolNameList {
						deleteIPPoolUntilFinish(poolName)
					}
				}
			})
		})
		It("Succeed to run daemonSet/pod who is cross-zone daemonSet with matched nodeSelector", Label("L00007"), func() {
			// generate  daemonSet yaml
			GinkgoWriter.Println("generate example daemonSet yaml")
			daemonSetYaml := common.GenerateExampleDaemonSetYaml(daemonSetName, namespace)
			Expect(daemonSetYaml).NotTo(BeNil(), "failed to generate daemonSet %v/%v yaml\n", namespace, daemonSetName)

			// set annotation to add ippool
			GinkgoWriter.Println("add annotations to daemonSet yaml")
			anno := types.AnnoPodIPPoolValue{}
			if frame.Info.IpV4Enabled {
				anno.IPv4Pools = allV4PoolNameList
			}
			if frame.Info.IpV6Enabled {
				anno.IPv6Pools = allV6PoolNameList
			}
			annoB, err := json.Marshal(anno)
			Expect(err).NotTo(HaveOccurred(), "failed to marshal pod annotations %+v\n", anno)
			annoStr := string(annoB)

			daemonSetYaml.Spec.Template.Annotations = map[string]string{
				constant.AnnoPodIPPool: annoStr,
			}
			GinkgoWriter.Printf("the daemonSet yaml is :%+v\n", daemonSetYaml)

			// create daemonSet
			GinkgoWriter.Printf("create daemonSet %v/%v \n", namespace, daemonSetName)
			Expect(frame.CreateDaemonSet(daemonSetYaml)).To(Succeed(), "failed to create daemonSet: %v/%v\n", namespace, daemonSetName)

			// wait daemonSet ready
			GinkgoWriter.Printf("wait daemonset %v/%v ready\n", namespace, daemonSetName)
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			daemonSet, err := frame.WaitDaemonSetReady(daemonSetName, namespace, ctx)
			Expect(err).NotTo(HaveOccurred(), "error: %v\n", err)
			Expect(daemonSet).NotTo(BeNil())

			// get podList
			GinkgoWriter.Printf("get pod list by label: %+v \n", daemonSet.Spec.Template.Labels)
			podList, err := frame.GetPodListByLabel(daemonSet.Spec.Template.Labels)
			Expect(err).NotTo(HaveOccurred(), "failed to get podList,error: %v \n", err)
			Expect(podList).NotTo(BeNil())

			// check pod ip in different node-ippool
			GinkgoWriter.Println("check pod ip if in different node-ippool")
			for _, pod := range podList.Items {
				ok, _, _, err := common.CheckPodIpRecordInIppool(frame, nodeV4PoolMap[pod.Spec.NodeName], nodeV6PoolMap[pod.Spec.NodeName], &corev1.PodList{Items: []corev1.Pod{pod}})
				Expect(err).NotTo(HaveOccurred(), "error: %v\n", err)
				Expect(ok).To(BeTrue())
			}

			// delete daemonSet
			GinkgoWriter.Printf("delete daemonSet %v/%v\n", namespace, daemonSetName)
			Expect(frame.DeleteDaemonSet(daemonSetName, namespace)).To(Succeed(), "failed to delete daemonSet %v/%v\n", namespace, daemonSetName)
			ctx2, cancel2 := context.WithTimeout(context.Background(), time.Minute)
			defer cancel2()
			Expect(frame.WaitPodListDeleted(namespace, daemonSet.Spec.Template.Labels, ctx2)).To(Succeed(), "time out to wait podList deleted\n")

			// check pod ip if reclaimed in different node-ippool
			GinkgoWriter.Println("check pod ip if reclaimed in different node-ippool")
			for _, pod := range podList.Items {
				_, ok, _, err := common.CheckPodIpRecordInIppool(frame, nodeV4PoolMap[pod.Spec.NodeName], nodeV6PoolMap[pod.Spec.NodeName], &corev1.PodList{Items: []corev1.Pod{pod}})
				Expect(err).NotTo(HaveOccurred(), "error: %v\n", err)
				Expect(ok).To(BeTrue())
			}
		})
	})
})

func deleteIPPoolUntilFinish(poolName string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	GinkgoWriter.Printf("delete ippool %v\n", poolName)
	Expect(common.DeleteIPPoolUntilFinish(frame, poolName, ctx)).To(Succeed(), "failed to delete ippool %v\n", poolName)
	GinkgoWriter.Printf("succeed to delete ippool %v\n", poolName)
}

func createIPPool(IPPoolObj *spiderpoolv1.SpiderIPPool) {
	GinkgoWriter.Printf("create ippool %v\n", IPPoolObj.Name)
	Expect(common.CreateIppool(frame, IPPoolObj)).To(Succeed())
	GinkgoWriter.Printf("succeeded to create ippool %v\n", IPPoolObj.Name)
}
