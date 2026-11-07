package cloud

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
)

// KafkaTopology — итоговые поля для проверки топологии. nil означает неверный тип
// в --body: зависимые от поля правила оставляются API.
type KafkaTopology struct {
	Brokers   *int32
	Subnets   *[]string
	Dedicated *bool
}

// Topology учитывает замену верхних ключей --body, как mergeBody.
func (c KafkaClusterCreate) Topology() (KafkaTopology, error) {
	body, err := parseTopologyBody(c.Body)
	if err != nil {
		return KafkaTopology{}, err
	}

	dedicated := ptr(c.Controller.Spec != "")
	if controller, exists := body.object("controller_spec"); exists {
		dedicated = nil
		if controller != nil {
			dedicated = ptr(true)
		}
	}

	return KafkaTopology{Brokers: body.number("brokers", c.Brokers), Subnets: body.subnets(c.Subnets), Dedicated: dedicated}, nil
}

// ClickHouseTopology — итоговая топология Keeper; поля неверного типа имеют nil.
type ClickHouseTopology struct {
	Shards, Replicas, KeeperStorageGiB *int32
	Subnets                            *[]string
	KeeperMode, KeeperSpec             *string
}

// Topology заменяет standalone_keeper целиком, без слияния его вложенных полей.
func (c ClickHouseClusterCreate) Topology() (ClickHouseTopology, error) {
	body, err := parseTopologyBody(c.Body)
	if err != nil {
		return ClickHouseTopology{}, err
	}

	mode := body.text("keeper_mode", c.KeeperMode)
	if _, overridden := body["keeper_mode"]; overridden && mode != nil {
		*mode = strings.TrimPrefix(*mode, "KEEPER_MODE_")
		if *mode == "UNSPECIFIED" {
			*mode = ""
		}
	}

	spec, storage := ptr(c.Keeper.Spec), ptr(c.Keeper.StorageGiB)
	if keeper, exists := body.object("standalone_keeper"); exists {
		spec, storage = nil, nil
		if keeper != nil {
			spec, storage = keeper.text("specification_id", ""), keeper.number("storage_gib", 0)
		}
	}

	return ClickHouseTopology{
		Shards: body.number("shards", c.Shards), Replicas: body.number("replicas", c.Replicas), Subnets: body.subnets(c.Subnets),
		KeeperMode: mode, KeeperSpec: spec, KeeperStorageGiB: storage,
	}, nil
}

type topologyBody map[string]json.RawMessage

func parseTopologyBody(raw []byte) (topologyBody, error) {
	var body topologyBody
	if raw != nil {
		if err := json.Unmarshal(raw, &body); err != nil {
			return nil, i18n.Errorf("cloud.err.body_object", err)
		}
	}

	return body, nil
}

func (b topologyBody) field(key string, target any) bool {
	raw, exists := b[key]
	return !exists || (!bytes.Equal(bytes.TrimSpace(raw), []byte("null")) && json.Unmarshal(raw, target) == nil)
}

func (b topologyBody) number(key string, fallback int32) *int32 {
	if !b.field(key, &fallback) {
		return nil
	}

	return &fallback
}

func (b topologyBody) text(key, fallback string) *string {
	if !b.field(key, &fallback) {
		return nil
	}

	return &fallback
}

func (b topologyBody) object(key string) (topologyBody, bool) {
	raw, exists := b[key]

	var object topologyBody
	if exists {
		if err := json.Unmarshal(raw, &object); err != nil {
			return nil, true
		}
	}

	return object, exists
}

func (b topologyBody) subnets(fallback []string) *[]string {
	if _, exists := b["subnet_ids"]; !exists {
		return &fallback
	}

	var values []any
	if !b.field("subnet_ids", &values) {
		return nil
	}

	subnets := make([]string, 0, len(values))
	for _, value := range values {
		subnet, ok := value.(string)
		if !ok {
			return nil
		}

		subnets = append(subnets, subnet)
	}

	return &subnets
}
