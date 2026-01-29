package service

import (
	"fmt"

	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sio "github.com/dell/goscaleio"
	siotypes "github.com/dell/goscaleio/types/v1"
)

// SystemCapacityCalculator is an interface for calculating system capacity.
type SystemCapacityCalculator interface {
	GetSystemCapacity(systemID string) (int64, error)
	GetStoragePoolCapacity(systemID, protectionDomain, spName string) (int64, error)
}

// PowerFlexGen1 implements SystemCapacityCalculator for Gen1 systems.
type PowerFlexGen1 struct {
	client *sio.Client
	system *sio.System
	s      *service
}

func (p *PowerFlexGen1) GetSystemCapacity(systemID string) (int64, error) {
	log.Debugf("Getting system capacity for system ID: %s", systemID)
	stats, err := p.system.GetStatistics()
	if err != nil {
		return 0, err
	}
	if !p.s.opts.Thick {
		return int64(stats.VolumeAllocationLimitInKb * bytesInKiB), nil
	}
	return int64(stats.CapacityAvailableForVolumeAllocationInKb * bytesInKiB), nil
}

func (p *PowerFlexGen1) GetStoragePoolCapacity(systemID, protectionDomain, spName string) (int64, error) {
	log.Debugf("Getting storage pool capacity for system ID: %s, storage pool: %s", systemID, spName)
	pdID, err := p.s.getProtectionDomainIDFromName(systemID, protectionDomain)
	if err != nil {
		return 0, err
	}
	sp, err := p.client.FindStoragePool(systemID, spName, "", pdID)
	if err != nil {
		return 0, status.Errorf(codes.Internal,
			"unable to look up storage pool: %s on system: %s, err: %s",
			spName, systemID, err.Error())
	}
	spc := sio.NewStoragePoolEx(p.client, sp)
	stats, err := spc.GetStatistics()
	if err != nil {
		return 0, err
	}

	if !p.s.opts.Thick {
		return int64(stats.VolumeAllocationLimitInKb * bytesInKiB), nil
	}

	return int64(stats.CapacityAvailableForVolumeAllocationInKb * bytesInKiB), nil
}

// PowerFlexGen2 implements SystemCapacityCalculator for Gen2 (GenTypeEC) systems.
type PowerFlexGen2 struct {
	client *sio.Client
	system *sio.System
	s      *service
}

func (p *PowerFlexGen2) GetSystemCapacity(systemID string) (int64, error) {
	log.Debugf("Getting system capacity for system ID: %s", systemID)
	metrics, err := p.client.GetMetrics("system", []string{systemID})
	if err != nil {
		return 0, status.Errorf(codes.Internal,
			"unable to get system stats on system: %s, err: %s", systemID, err.Error())
	}

	if len(metrics.Resources) == 0 {
		return 0, fmt.Errorf("no metrics found for system %s", systemID)
	}
	return int64(getMetric(metrics.Resources[0].Metrics, "physical_free")), nil
}

func (p *PowerFlexGen2) GetStoragePoolCapacity(systemID, protectionDomain, spName string) (int64, error) {
	log.Debugf("Getting storage pool capacity for system ID: %s, storage pool: %s", systemID, spName)
	pdID, err := p.s.getProtectionDomainIDFromName(systemID, protectionDomain)
	if err != nil {
		return 0, err
	}

	sp, err := p.client.FindStoragePool(systemID, spName, "", pdID)
	if err != nil {
		log.Errorf("Error finding storage pool: %s", err)
		return 0, status.Errorf(codes.Internal,
			"unable to look up storage pool: %s on system: %s, err: %s",
			spName, systemID, err.Error())
	}

	metrics, err := p.client.GetMetrics("storage_pool", []string{sp.ID})
	if err != nil {
		return 0, err
	}

	if len(metrics.Resources) == 0 {
		return 0, fmt.Errorf("no metrics found for storage pool %s", spName)
	}

	return int64(getMetric(metrics.Resources[0].Metrics, "physical_free")), nil
}

// getSystemCapacityCalculator returns a SystemCapacityCalculator based on the system's generation type.
func (s *service) getSystemCapacityCalculator(systemID string, service *service) (SystemCapacityCalculator, error) {
	adminClient := s.adminClients[systemID]
	system := s.systems[systemID]
	if adminClient == nil || system == nil {
		return nil, fmt.Errorf("can't find adminClient or system by id %s", systemID)
	}

	platformInfo, err := s.GetPlatformInfo(systemID)
	if err != nil {
		return nil, err
	}

	if platformInfo.GenType == siotypes.GenTypeEC {
		log.Infof("GetType: %s", siotypes.GenTypeEC)
		return &PowerFlexGen2{client: adminClient, system: system, s: service}, nil
	}
	return &PowerFlexGen1{client: adminClient, system: system, s: service}, nil
}

// Gets capacity of a given storage system. When storage pool name is provided, gets capcity of this storage pool only.
func (s *service) getSystemCapacity(ctx context.Context, systemID, protectionDomain string, spName ...string) (int64, error) {
	log.Infof("Get capacity for system: %s, pool %s", systemID, spName)

	if err := s.requireProbe(ctx, systemID); err != nil {
		return 0, err
	}

	calculator, err := s.getSystemCapacityCalculator(systemID, s)
	if err != nil {
		return 0, err
	}
	if len(spName) > 0 && spName[0] != "" {
		return calculator.GetStoragePoolCapacity(systemID, protectionDomain, spName[0])
	}
	return calculator.GetSystemCapacity(systemID)
}
