// Copyright 2022 Authors of spidernet-io
// SPDX-License-Identifier: Apache-2.0

package ippoolmanager

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apitypes "k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/spidernet-io/spiderpool/api/v1/agent/models"
	"github.com/spidernet-io/spiderpool/pkg/constant"
	"github.com/spidernet-io/spiderpool/pkg/election"
	spiderpoolip "github.com/spidernet-io/spiderpool/pkg/ip"
	spiderpoolv1 "github.com/spidernet-io/spiderpool/pkg/k8s/apis/v1"
	"github.com/spidernet-io/spiderpool/pkg/logutils"
	"github.com/spidernet-io/spiderpool/pkg/namespacemanager"
	"github.com/spidernet-io/spiderpool/pkg/nodemanager"
	"github.com/spidernet-io/spiderpool/pkg/podmanager"
	"github.com/spidernet-io/spiderpool/pkg/reservedipmanager"
	"github.com/spidernet-io/spiderpool/pkg/types"
)

var logger = logutils.Logger.Named("IPPool-Manager")

type IPPoolManager interface {
	GetIPPoolByName(ctx context.Context, poolName string) (*spiderpoolv1.SpiderIPPool, error)
	ListIPPools(ctx context.Context, opts ...client.ListOption) (*spiderpoolv1.SpiderIPPoolList, error)
	AllocateIP(ctx context.Context, poolName, containerID, nic string, pod *corev1.Pod) (*models.IPConfig, *spiderpoolv1.SpiderIPPool, error)
	ReleaseIP(ctx context.Context, poolName string, ipAndCIDs []IPAndCID) error
	SelectByPod(ctx context.Context, version types.IPVersion, poolName string, pod *corev1.Pod) (bool, error)
	CheckVlanSame(ctx context.Context, poolNameList []string) (map[types.Vlan][]string, bool, error)
	RemoveFinalizer(ctx context.Context, poolName string) error
	AssembleTotalIPs(ctx context.Context, ipPool *spiderpoolv1.SpiderIPPool) ([]net.IP, error)
	SetupReconcile(leader election.SpiderLeaseElector) error
	SetupWebhook() error
	UpdateAllocatedIPs(ctx context.Context, containerID string, pod *corev1.Pod, oldIPConfig models.IPConfig) error
}

type ipPoolManager struct {
	client                client.Client
	runtimeMgr            ctrl.Manager
	rIPManager            reservedipmanager.ReservedIPManager
	nodeManager           nodemanager.NodeManager
	nsManager             namespacemanager.NamespaceManager
	podManager            podmanager.PodManager
	maxAllocatedIPs       int
	maxConflictRetrys     int
	conflictRetryUnitTime time.Duration
	leader                election.SpiderLeaseElector
}

func NewIPPoolManager(mgr ctrl.Manager, rIPManager reservedipmanager.ReservedIPManager, nodeManager nodemanager.NodeManager, nsManager namespacemanager.NamespaceManager, podManager podmanager.PodManager, maxAllocatedIPs, maxConflictRetrys int, conflictRetryUnitTime time.Duration) (IPPoolManager, error) {
	if mgr == nil {
		return nil, errors.New("k8s manager must be specified")
	}
	if rIPManager == nil {
		return nil, errors.New("ReservedIPManager must be specified")
	}
	if nodeManager == nil {
		return nil, errors.New("NodeManager must be specified")
	}
	if nsManager == nil {
		return nil, errors.New("NamespaceManager must be specified")
	}
	if podManager == nil {
		return nil, errors.New("PodManager must be specified")
	}

	return &ipPoolManager{
		client:                mgr.GetClient(),
		runtimeMgr:            mgr,
		rIPManager:            rIPManager,
		nodeManager:           nodeManager,
		nsManager:             nsManager,
		podManager:            podManager,
		maxAllocatedIPs:       maxAllocatedIPs,
		maxConflictRetrys:     maxConflictRetrys,
		conflictRetryUnitTime: conflictRetryUnitTime,
	}, nil
}

func (im *ipPoolManager) GetIPPoolByName(ctx context.Context, poolName string) (*spiderpoolv1.SpiderIPPool, error) {
	var ipPool spiderpoolv1.SpiderIPPool
	if err := im.client.Get(ctx, apitypes.NamespacedName{Name: poolName}, &ipPool); err != nil {
		return nil, err
	}

	return &ipPool, nil
}

func (im *ipPoolManager) ListIPPools(ctx context.Context, opts ...client.ListOption) (*spiderpoolv1.SpiderIPPoolList, error) {
	ipPoolList := &spiderpoolv1.SpiderIPPoolList{}
	if err := im.client.List(ctx, ipPoolList, opts...); err != nil {
		return nil, err
	}

	return ipPoolList, nil
}

func (im *ipPoolManager) AllocateIP(ctx context.Context, poolName, containerID, nic string, pod *corev1.Pod) (*models.IPConfig, *spiderpoolv1.SpiderIPPool, error) {
	// TODO(iiiceoo): STS static ip, check "EnableStatuflsetIP"

	var ipConfig *models.IPConfig
	var usedIPPool *spiderpoolv1.SpiderIPPool
	rand.Seed(time.Now().UnixNano())
	for i := 0; i <= im.maxConflictRetrys; i++ {
		ipPool, err := im.GetIPPoolByName(ctx, poolName)
		if err != nil {
			return nil, nil, err
		}

		allocatedIP, err := im.genRandomIP(ctx, ipPool)
		if err != nil {
			return nil, nil, err
		}

		// TODO(iiiceoo): Remove when Defaulter webhook work
		if ipPool.Status.AllocatedIPs == nil {
			ipPool.Status.AllocatedIPs = spiderpoolv1.PoolIPAllocations{}
		}

		ipPool.Status.AllocatedIPs[allocatedIP.String()] = spiderpoolv1.PoolIPAllocation{
			ContainerID:         containerID,
			NIC:                 nic,
			Node:                pod.Spec.NodeName,
			Namespace:           pod.Namespace,
			Pod:                 pod.Name,
			OwnerControllerType: podmanager.GetControllerOwnerType(pod),
		}

		// TODO(iiiceoo): Remove when Defaulter webhook work
		if ipPool.Status.AllocatedIPCount == nil {
			ipPool.Status.AllocatedIPCount = new(int64)
		}

		*ipPool.Status.AllocatedIPCount++
		if *ipPool.Status.AllocatedIPCount > int64(im.maxAllocatedIPs) {
			return nil, nil, fmt.Errorf("threshold of IP allocations(<=%d) for IPPool exceeded: %w", im.maxAllocatedIPs, constant.ErrIPUsedOut)
		}

		if err := im.client.Status().Update(ctx, ipPool); err != nil {
			if !apierrors.IsConflict(err) {
				return nil, nil, err
			}
			if i == im.maxConflictRetrys {
				return nil, nil, fmt.Errorf("insufficient retries(<=%d) to allocate IP from IPPool %s", im.maxConflictRetrys, poolName)
			}
			time.Sleep(time.Duration(rand.Intn(1<<(i+1))) * im.conflictRetryUnitTime)
			continue
		}

		usedIPPool = ipPool
		ipConfig, err = genResIPConfig(allocatedIP, &ipPool.Spec, nic, poolName)
		if err != nil {
			return nil, nil, err
		}
		break
	}

	return ipConfig, usedIPPool, nil
}

func (im *ipPoolManager) genRandomIP(ctx context.Context, ipPool *spiderpoolv1.SpiderIPPool) (net.IP, error) {
	rIPList, err := im.rIPManager.ListReservedIPs(ctx)
	if err != nil {
		return nil, err
	}
	reservedIPs, err := im.rIPManager.GetReservedIPsByIPVersion(ctx, *ipPool.Spec.IPVersion, rIPList)
	if err != nil {
		return nil, err
	}

	var used []string
	for ip := range ipPool.Status.AllocatedIPs {
		used = append(used, ip)
	}
	usedIPs, err := spiderpoolip.ParseIPRanges(*ipPool.Spec.IPVersion, used)
	if err != nil {
		return nil, err
	}

	expectIPs, err := spiderpoolip.ParseIPRanges(*ipPool.Spec.IPVersion, ipPool.Spec.IPs)
	if err != nil {
		return nil, err
	}
	excludeIPs, err := spiderpoolip.ParseIPRanges(*ipPool.Spec.IPVersion, ipPool.Spec.ExcludeIPs)
	if err != nil {
		return nil, err
	}
	availableIPs := spiderpoolip.IPsDiffSet(expectIPs, append(reservedIPs, append(usedIPs, excludeIPs...)...))

	if len(availableIPs) == 0 {
		return nil, constant.ErrIPUsedOut
	}

	return availableIPs[rand.Int()%len(availableIPs)], nil
}

type IPAndCID struct {
	IP          string
	ContainerID string
}

func (im *ipPoolManager) ReleaseIP(ctx context.Context, poolName string, ipAndCIDs []IPAndCID) error {
	rand.Seed(time.Now().UnixNano())
	for i := 0; i <= im.maxConflictRetrys; i++ {
		ipPool, err := im.GetIPPoolByName(ctx, poolName)
		if err != nil {
			return err
		}

		// TODO(iiiceoo): Remove when Defaulter webhook work
		if ipPool.Status.AllocatedIPs == nil {
			ipPool.Status.AllocatedIPs = spiderpoolv1.PoolIPAllocations{}
		}
		if ipPool.Status.AllocatedIPCount == nil {
			ipPool.Status.AllocatedIPCount = new(int64)
		}

		needRelease := false
		for _, e := range ipAndCIDs {
			if a, ok := ipPool.Status.AllocatedIPs[e.IP]; ok {
				if a.ContainerID == e.ContainerID {
					delete(ipPool.Status.AllocatedIPs, e.IP)
					*ipPool.Status.AllocatedIPCount--
					needRelease = true
				}
			}
		}

		if !needRelease {
			return nil
		}

		if err := im.client.Status().Update(ctx, ipPool); err != nil {
			if !apierrors.IsConflict(err) {
				return err
			}
			if i == im.maxConflictRetrys {
				return fmt.Errorf("insufficient retries(<=%d) to release IP %+v from IPPool %s", im.maxConflictRetrys, ipAndCIDs, poolName)
			}
			time.Sleep(time.Duration(rand.Intn(1<<(i+1))) * im.conflictRetryUnitTime)
			continue
		}
		break
	}

	return nil
}

func (im *ipPoolManager) SelectByPod(ctx context.Context, version types.IPVersion, poolName string, pod *corev1.Pod) (bool, error) {
	logger := logutils.FromContext(ctx)

	ipPool, err := im.GetIPPoolByName(ctx, poolName)
	if err != nil {
		logger.Sugar().Warnf("Failed to get IPPool %s: %v", poolName, err)
		return false, err
	}

	if ipPool.DeletionTimestamp != nil {
		logger.Sugar().Warnf("IPPool %s is terminating", poolName)
		return false, nil
	}

	if *ipPool.Spec.Disable {
		logger.Sugar().Warnf("IPPool %s is disable", poolName)
		return false, nil
	}

	if *ipPool.Spec.IPVersion != version {
		logger.Sugar().Warnf("IPPool %s has different version with specified via input", poolName)
		return false, nil
	}

	// TODO(iiiceoo): Check whether there are any unused IP

	if ipPool.Spec.NodeAffinity != nil {
		nodeMatched, err := im.nodeManager.MatchLabelSelector(ctx, pod.Spec.NodeName, ipPool.Spec.NodeAffinity)
		if err != nil {
			return false, err
		}
		if !nodeMatched {
			logger.Sugar().Infof("Unmatched Node selector, IPPool %s is filtered", poolName)
			return false, nil
		}
	}

	if ipPool.Spec.NamesapceAffinity != nil {
		nsMatched, err := im.nsManager.MatchLabelSelector(ctx, pod.Namespace, ipPool.Spec.NamesapceAffinity)
		if err != nil {
			return false, err
		}
		if !nsMatched {
			logger.Sugar().Infof("Unmatched Namespace selector, IPPool %s is filtered", poolName)
			return false, nil
		}
	}

	if ipPool.Spec.PodAffinity != nil {
		podMatched, err := im.podManager.MatchLabelSelector(ctx, pod.Namespace, pod.Name, ipPool.Spec.PodAffinity)
		if err != nil {
			return false, err
		}
		if !podMatched {
			logger.Sugar().Infof("Unmatched Pod selector, IPPool %s is filtered", poolName)
			return false, nil
		}
	}

	return true, nil
}

// TODO(iiiceoo): Refactor
func (im *ipPoolManager) CheckVlanSame(ctx context.Context, poolNameList []string) (map[types.Vlan][]string, bool, error) {
	vlanToPools := map[types.Vlan][]string{}
	for _, poolName := range poolNameList {
		ipPool, err := im.GetIPPoolByName(ctx, poolName)
		if err != nil {
			return nil, false, err
		}

		vlanToPools[*ipPool.Spec.Vlan] = append(vlanToPools[*ipPool.Spec.Vlan], poolName)
	}

	if len(vlanToPools) > 1 {
		return vlanToPools, false, nil
	}

	return vlanToPools, true, nil
}

func (im *ipPoolManager) RemoveFinalizer(ctx context.Context, poolName string) error {
	for i := 0; i <= im.maxConflictRetrys; i++ {
		ipPool, err := im.GetIPPoolByName(ctx, poolName)
		if err != nil {
			return err
		}

		if !controllerutil.ContainsFinalizer(ipPool, constant.SpiderFinalizer) {
			return nil
		}

		controllerutil.RemoveFinalizer(ipPool, constant.SpiderFinalizer)
		if err := im.client.Update(ctx, ipPool); err != nil {
			if !apierrors.IsConflict(err) {
				return err
			}
			if i == im.maxConflictRetrys {
				return fmt.Errorf("insufficient retries(<=%d) to remove finalizer '%s' from IPPool %s", im.maxConflictRetrys, constant.SpiderFinalizer, poolName)
			}
			time.Sleep(time.Duration(rand.Intn(1<<(i+1))) * im.conflictRetryUnitTime)
			continue
		}
		break
	}

	return nil
}

// AssembleTotalIP will calculate an IPPool CR object usable IPs number,
// it summaries the IPPool IPs then subtracts ExcludeIPs.
// notice: this method would not filter ReservedIP CR object data!
func (im *ipPoolManager) AssembleTotalIPs(ctx context.Context, ipPool *spiderpoolv1.SpiderIPPool) ([]net.IP, error) {
	// TODO (Icarus9913): ips could be nil, should we return error?
	ips, err := spiderpoolip.ParseIPRanges(*ipPool.Spec.IPVersion, ipPool.Spec.IPs)
	if nil != err {
		return nil, err
	}
	excludeIPs, err := spiderpoolip.ParseIPRanges(*ipPool.Spec.IPVersion, ipPool.Spec.ExcludeIPs)
	if nil != err {
		return nil, err
	}
	usableIPs := spiderpoolip.IPsDiffSet(ips, excludeIPs)

	return usableIPs, nil
}

// UpdateAllocatedIPs serves for StatefulSet pod re-create
func (im *ipPoolManager) UpdateAllocatedIPs(ctx context.Context, containerID string, pod *corev1.Pod, oldIPConfig models.IPConfig) error {
	for i := 0; i <= im.maxConflictRetrys; i++ {
		pool, err := im.GetIPPoolByName(ctx, oldIPConfig.IPPool)
		if nil != err {
			return err
		}

		// switch CIDR to IP
		ipAndCIDR := *oldIPConfig.Address
		singleIP, _, _ := strings.Cut(ipAndCIDR, "/")

		// basically, we just need to update ContainerID and Node.
		pool.Status.AllocatedIPs[singleIP] = spiderpoolv1.PoolIPAllocation{
			ContainerID:         containerID,
			NIC:                 *oldIPConfig.Nic,
			Node:                pod.Spec.NodeName,
			Namespace:           pod.Namespace,
			Pod:                 pod.Name,
			OwnerControllerType: constant.OwnerStatefulSet,
		}

		err = im.client.Status().Update(ctx, pool)
		if nil != err {
			if !apierrors.IsConflict(err) {
				return err
			}

			if i == im.maxConflictRetrys {
				return fmt.Errorf("insufficient retries(<=%d) to re-allocate StatefulSet pod '%s/%s' SpiderIPPool IP '%s'", im.maxConflictRetrys, pod.Namespace, pod.Name, singleIP)
			}

			time.Sleep(time.Duration(rand.Intn(1<<(i+1))) * im.conflictRetryUnitTime)
			continue
		}
	}
	return nil
}
