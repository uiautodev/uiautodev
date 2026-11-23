package webserver

import (
	"github.com/uiautodev/uiautodev/webserver/cluster"
)

type configClusterStore struct{}

func newClusterStore() cluster.Store { return &configClusterStore{} }

func (s *configClusterStore) ListSlaves() ([]cluster.SlaveRecord, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	if cfg.Cluster == nil || len(cfg.Cluster.Slaves) == 0 {
		return nil, nil
	}
	out := make([]cluster.SlaveRecord, 0, len(cfg.Cluster.Slaves))
	for _, record := range cfg.Cluster.Slaves {
		out = append(out, record)
	}
	return out, nil
}

func (s *configClusterStore) GetSlave(id string) (cluster.SlaveRecord, bool, error) {
	cfg, err := loadConfig()
	if err != nil {
		return cluster.SlaveRecord{}, false, err
	}
	if cfg.Cluster == nil {
		return cluster.SlaveRecord{}, false, nil
	}
	record, ok := cfg.Cluster.Slaves[id]
	return record, ok, nil
}

func (s *configClusterStore) SaveSlave(record cluster.SlaveRecord) error {
	return mutateConfig(func(cfg *appConfig) {
		if cfg.Cluster == nil {
			cfg.Cluster = &ClusterConfig{}
		}
		if cfg.Cluster.Slaves == nil {
			cfg.Cluster.Slaves = make(map[string]cluster.SlaveRecord)
		}
		cfg.Cluster.Slaves[record.ID] = record
	})
}

func (s *configClusterStore) DeleteSlave(id string) error {
	return mutateConfig(func(cfg *appConfig) {
		if cfg.Cluster == nil {
			return
		}
		delete(cfg.Cluster.Slaves, id)
	})
}

func getClusterADBRelayConfig() cluster.ADBRelayConfig {
	cfg, err := loadConfig()
	if err != nil || cfg.Cluster == nil || cfg.Cluster.ADBRelay == nil {
		return cluster.DefaultADBRelayConfig()
	}
	return *cfg.Cluster.ADBRelay
}
