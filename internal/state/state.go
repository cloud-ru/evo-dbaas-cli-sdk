// Package state — зависимости команд: вывод, проект,
// ожидание операций и клиенты API. Клиенты — функции: строятся при первом
// вызове, поэтому version, help и completion работают без ключа.
package state

import (
	"time"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
)

// ErrNoProject — проект не задан ни флагом, ни в окружении.
var ErrNoProject = i18n.Sentinel("state.err.no_project")

// State заполняет корневая команда до запуска подкоманды.
type State struct {
	Printer *output.Printer
	Project string
	// Timeout — предел --wait, PollDelay — шаг опроса операции.
	Timeout, PollDelay time.Duration
	// Env — переменная окружения (пароль из --password-env); читает её config.
	Env func(name string) string

	// ClickHouseURL — адрес API ClickHouse; флаг --api-url продукта пишет сюда.
	ClickHouseURL string
	ClickHouse    func() (*cloud.ClickHouse, error)
	// RedisURL — адрес API Redis; флаг --api-url продукта пишет сюда.
	RedisURL string
	Redis    func() (*cloud.Redis, error)
	// KafkaURL — адрес API Kafka; флаг --api-url продукта пишет сюда.
	KafkaURL string
	Kafka    func() (*cloud.Kafka, error)
	Compute  func() (*cloud.Compute, error)
	VPC      func() (*cloud.VPC, error)
	IAM      func() (*cloud.IAM, error)
}

// NeedProject — проект или ErrNoProject.
func (s *State) NeedProject() (string, error) {
	if s.Project == "" {
		return "", ErrNoProject
	}

	return s.Project, nil
}
