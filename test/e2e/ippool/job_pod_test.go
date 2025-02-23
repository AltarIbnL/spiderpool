// Copyright 2022 Authors of spidernet-io
// SPDX-License-Identifier: Apache-2.0
package ippool_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spidernet-io/e2eframework/tools"
	"github.com/spidernet-io/spiderpool/test/e2e/common"
	"k8s.io/utils/pointer"
)

var _ = Describe("test ip with Job case", Label("Job"), func() {

	var jdName, nsName string

	BeforeEach(func() {

		// init namespace name and create
		nsName = "ns" + tools.RandomName()
		GinkgoWriter.Printf("create namespace %v \n", nsName)
		err := frame.CreateNamespace(nsName)
		Expect(err).NotTo(HaveOccurred(), "failed to create namespace %v", nsName)

		// init Job name
		jdName = "jd" + tools.RandomName()

		// clean test env
		DeferCleanup(func() {
			GinkgoWriter.Printf("delete namespace %v \n", nsName)
			err = frame.DeleteNamespace(nsName)
			Expect(err).NotTo(HaveOccurred(), "failed to delete namespace %v", nsName)
		})
	})

	It("one Job 2 pods allocate/release ipv4 and ipv6 addresses", Label("smoke", "E00005"), func() {

		// create Job
		GinkgoWriter.Printf("try to create Job %v/%v \n", jdName, nsName)

		behavior := common.JobTypeRunningForever
		jd := common.GenerateExampleJobYaml(behavior, jdName, nsName, pointer.Int32Ptr(2))
		Expect(jd).NotTo(BeNil())
		label := jd.Spec.Template.Labels
		parallelism := jd.Spec.Parallelism

		GinkgoWriter.Printf("job yaml:\n %v \n", jd)

		e1 := frame.CreateJob(jd)
		Expect(e1).NotTo(HaveOccurred(), "failed to create job \n")

		// wait job pod list running
		GinkgoWriter.Printf("wait job pod list running \n")
		ctx1, cancel1 := context.WithTimeout(context.Background(), time.Minute)
		defer cancel1()
		e2 := frame.WaitPodListRunning(label, int(*parallelism), ctx1)
		Expect(e2).NotTo(HaveOccurred())

		// get job pod list
		GinkgoWriter.Printf("get job pod list \n")
		podlist, e3 := frame.GetJobPodList(jd)
		Expect(e3).NotTo(HaveOccurred())
		Expect(podlist).NotTo(BeNil())

		err := frame.CheckPodListIpReady(podlist)
		Expect(err).NotTo(HaveOccurred(), "failed to check ipv4 or ipv6")
		GinkgoWriter.Printf("succeeded to assign ipv4、ipv6 ip for pod %v/%v \n", nsName, jdName)

		// delete job
		GinkgoWriter.Printf("delete job: %v \n", jdName)
		err = frame.DeleteJob(jdName, nsName)
		Expect(err).NotTo(HaveOccurred(), "failed to delete job: %v \n", jdName)
	})

	DescribeTable("check ip release after job finished",

		func(behavior common.JobBehave) {
			// create Job
			GinkgoWriter.Printf("try to create Job %v/%v \n", nsName, jdName)
			jd := common.GenerateExampleJobYaml(behavior, jdName, nsName, pointer.Int32Ptr(2))
			Expect(jd).NotTo(BeNil())
			GinkgoWriter.Printf("job behavior:\n %v \n", behavior)
			e1 := frame.CreateJob(jd)
			Expect(e1).NotTo(HaveOccurred(), "failed to create job \n")

			// wait job finished
			GinkgoWriter.Printf("wait job finished \n")
			ctx1, cancel1 := context.WithTimeout(context.Background(), time.Minute)
			defer cancel1()
			jb, ok1, e3 := frame.WaitJobFinished(jdName, nsName, ctx1)
			Expect(e3).NotTo(HaveOccurred(), "failed to wait job finished: %v\n", e3)
			Expect(jb).NotTo(BeNil())

			switch behavior {
			case common.JobTypeFail:
				Expect(ok1).To(BeFalse())
			case common.JobTypeFinish:
				Expect(ok1).To(BeTrue())
			default:
				Fail("input error")
			}

			GinkgoWriter.Printf("job %v is finished \n job conditions:\n %v \n", jb, jb.Status.Conditions)

			// TODO(weiyang) check ip release

			// delete job
			GinkgoWriter.Printf("delete job: %v \n", jdName)
			err := frame.DeleteJob(jdName, nsName)
			Expect(err).NotTo(HaveOccurred(), "failed to delete job: %v \n", jdName)

		},
		Entry("check ip release when job is failed", Label("E00005"), common.JobTypeFail),
		Entry("check ip release when job is succeeded", Label("E00005"), common.JobTypeFinish),

		// TODO(yangwei) check to release
	)
})
