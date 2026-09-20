// Package gateway 提供知华边缘网关的协议归一、规则引擎和断点续传能力。
// 上海如静知华信息科技有限公司：https://www.zhuatech.cn/
// 商业授权或定制开发请微信添加微信号zhuatech或zhuatech2进行咨询。
package gateway

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Adapter 是工业协议接入配置。社区版内置模拟器，真实驱动通过同一端口替换。
type Adapter struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Protocol   string `json:"protocol"`
	Endpoint   string `json:"endpoint"`
	Status     string `json:"status"`
	LastPollAt string `json:"lastPollAt,omitempty"`
}

// Point 是协议地址到统一测点模型的映射。
type Point struct {
	ID        string  `json:"id"`
	AdapterID string  `json:"adapterId"`
	Code      string  `json:"code"`
	Address   string  `json:"address"`
	Unit      string  `json:"unit"`
	Scale     float64 `json:"scale"`
	Value     float64 `json:"value"`
	Quality   string  `json:"quality"`
	UpdatedAt string  `json:"updatedAt,omitempty"`
}

// Rule 定义本地、断网仍可运行的阈值规则。
type Rule struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	PointCode string  `json:"pointCode"`
	Operator  string  `json:"operator"`
	Threshold float64 `json:"threshold"`
	Action    string  `json:"action"`
	Enabled   bool    `json:"enabled"`
}

// Event 是规则命中后产生的本地事件。
type Event struct {
	ID         string  `json:"id"`
	RuleID     string  `json:"ruleId"`
	PointCode  string  `json:"pointCode"`
	Value      float64 `json:"value"`
	Action     string  `json:"action"`
	OccurredAt string  `json:"occurredAt"`
}

// OutboxItem 表示等待向云端可靠投递的数据批次。
type OutboxItem struct {
	ID        string         `json:"id"`
	Kind      string         `json:"kind"`
	Payload   map[string]any `json:"payload"`
	Attempts  int            `json:"attempts"`
	Status    string         `json:"status"`
	CreatedAt string         `json:"createdAt"`
	AckedAt   string         `json:"ackedAt,omitempty"`
}

// ConfigRevision 表示带摘要的边缘配置版本。
type ConfigRevision struct {
	Revision  int64  `json:"revision"`
	Checksum  string `json:"checksum"`
	AppliedAt string `json:"appliedAt"`
	AppliedBy string `json:"appliedBy"`
}

// Snapshot 是可持久化的网关状态。
type Snapshot struct {
	Adapters    []Adapter        `json:"adapters"`
	Points      []Point          `json:"points"`
	Rules       []Rule           `json:"rules"`
	Events      []Event          `json:"events"`
	Outbox      []OutboxItem     `json:"outbox"`
	Config      ConfigRevision   `json:"config"`
	CloudOnline bool             `json:"cloudOnline"`
	Audit       []map[string]any `json:"audit"`
}

// Engine 是线程安全的边缘规则与存储转发引擎。
type Engine struct {
	mu          sync.RWMutex
	adapters    map[string]*Adapter
	points      map[string]*Point
	rules       map[string]*Rule
	events      []Event
	outbox      map[string]*OutboxItem
	config      ConfigRevision
	cloudOnline bool
	audit       []map[string]any
}

// NewEngine 从状态快照创建引擎。
func NewEngine(s Snapshot) *Engine {
	e := &Engine{adapters: map[string]*Adapter{}, points: map[string]*Point{}, rules: map[string]*Rule{}, outbox: map[string]*OutboxItem{}, events: s.Events, config: s.Config, cloudOnline: s.CloudOnline, audit: s.Audit}
	for i := range s.Adapters {
		v := s.Adapters[i]
		e.adapters[v.ID] = &v
	}
	for i := range s.Points {
		v := s.Points[i]
		e.points[v.Code] = &v
	}
	for i := range s.Rules {
		v := s.Rules[i]
		e.rules[v.ID] = &v
	}
	for i := range s.Outbox {
		v := s.Outbox[i]
		e.outbox[v.ID] = &v
	}
	return e
}
func makeID(p string) string {
	b := make([]byte, 7)
	_, _ = rand.Read(b)
	return p + "_" + hex.EncodeToString(b)
}
func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// AddAdapter 注册协议适配器，目前支持 mqtt、modbus-tcp、opc-ua 和 simulator 配置。
func (e *Engine) AddAdapter(name, protocol, endpoint string) (Adapter, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	allowed := map[string]bool{"mqtt": true, "modbus-tcp": true, "opc-ua": true, "simulator": true}
	if strings.TrimSpace(name) == "" || !allowed[protocol] {
		return Adapter{}, errors.New("适配器名称或协议无效")
	}
	a := &Adapter{ID: makeID("adp"), Name: name, Protocol: protocol, Endpoint: endpoint, Status: "configured"}
	e.adapters[a.ID] = a
	e.record("ADAPTER_ADDED", a.ID)
	return *a, nil
}

// AddPoint 新增统一测点映射。
func (e *Engine) AddPoint(adapterID, code, address, unit string, scale float64) (Point, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.adapters[adapterID]; !ok {
		return Point{}, errors.New("适配器不存在")
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return Point{}, errors.New("测点编码不能为空")
	}
	if _, exists := e.points[code]; exists {
		return Point{}, errors.New("测点编码已存在")
	}
	if scale == 0 {
		scale = 1
	}
	p := &Point{ID: makeID("pt"), AdapterID: adapterID, Code: code, Address: address, Unit: unit, Scale: scale, Quality: "unknown"}
	e.points[code] = p
	e.record("POINT_ADDED", p.ID)
	return *p, nil
}

// AddRule 创建可离线执行的本地阈值规则。
func (e *Engine) AddRule(name, pointCode, operator string, threshold float64, action string) (Rule, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.points[pointCode]; !ok {
		return Rule{}, errors.New("测点不存在")
	}
	if operator != "gt" && operator != "gte" && operator != "lt" && operator != "lte" {
		return Rule{}, errors.New("规则运算符无效")
	}
	r := &Rule{ID: makeID("rul"), Name: name, PointCode: pointCode, Operator: operator, Threshold: threshold, Action: action, Enabled: true}
	e.rules[r.ID] = r
	e.record("RULE_ADDED", r.ID)
	return *r, nil
}

// Ingest 将原始值缩放、执行规则，并写入可靠投递箱。
func (e *Engine) Ingest(pointCode string, raw float64, quality string) (Point, []Event, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	p, ok := e.points[pointCode]
	if !ok {
		return Point{}, nil, errors.New("测点不存在")
	}
	if quality == "" {
		quality = "good"
	}
	p.Value = raw * p.Scale
	p.Quality = quality
	p.UpdatedAt = now()
	if a := e.adapters[p.AdapterID]; a != nil {
		a.Status = "online"
		a.LastPollAt = p.UpdatedAt
	}
	item := &OutboxItem{ID: makeID("out"), Kind: "telemetry", Payload: map[string]any{"pointCode": pointCode, "value": p.Value, "quality": quality, "occurredAt": p.UpdatedAt}, Status: "pending", CreatedAt: now()}
	e.outbox[item.ID] = item
	triggered := []Event{}
	if quality == "good" {
		for _, r := range e.rules {
			if r.Enabled && r.PointCode == pointCode && matches(p.Value, r.Operator, r.Threshold) {
				event := Event{ID: makeID("evt"), RuleID: r.ID, PointCode: pointCode, Value: p.Value, Action: r.Action, OccurredAt: now()}
				e.events = append(e.events, event)
				triggered = append(triggered, event)
				oi := &OutboxItem{ID: makeID("out"), Kind: "event", Payload: map[string]any{"eventId": event.ID, "action": event.Action, "value": event.Value}, Status: "pending", CreatedAt: now()}
				e.outbox[oi.ID] = oi
			}
		}
	}
	return *p, triggered, nil
}
func matches(v float64, op string, t float64) bool {
	switch op {
	case "gt":
		return v > t
	case "gte":
		return v >= t
	case "lt":
		return v < t
	default:
		return v <= t
	}
}

// SetCloudOnline 更新上云链路状态。
func (e *Engine) SetCloudOnline(online bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cloudOnline = online
	e.record("CLOUD_LINK_CHANGED", fmt.Sprint(online))
}

// PendingBatch 取得待投递数据并累计尝试次数；断网时不丢弃数据。
func (e *Engine) PendingBatch(limit int) ([]OutboxItem, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.cloudOnline {
		return nil, errors.New("云端链路离线")
	}
	if limit < 1 {
		limit = 100
	}
	items := []*OutboxItem{}
	for _, item := range e.outbox {
		if item.Status == "pending" {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt < items[j].CreatedAt })
	if len(items) > limit {
		items = items[:limit]
	}
	result := make([]OutboxItem, 0, len(items))
	for _, item := range items {
		item.Attempts++
		result = append(result, *item)
	}
	return result, nil
}

// Ack 确认云端已接收，重复确认安全。
func (e *Engine) Ack(ids []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, id := range ids {
		if item := e.outbox[id]; item != nil {
			item.Status = "acked"
			item.AckedAt = now()
		}
	}
}

// ApplyConfig 应用带摘要的配置修订，拒绝旧版本覆盖。
func (e *Engine) ApplyConfig(revision int64, body, checksum, actor string) (ConfigRevision, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if revision <= e.config.Revision {
		return ConfigRevision{}, errors.New("配置版本必须递增")
	}
	sum := sha256.Sum256([]byte(body))
	actual := hex.EncodeToString(sum[:])
	if actual != checksum {
		return ConfigRevision{}, errors.New("配置校验失败")
	}
	e.config = ConfigRevision{Revision: revision, Checksum: actual, AppliedAt: now(), AppliedBy: actor}
	e.record("CONFIG_APPLIED", fmt.Sprint(revision))
	return e.config, nil
}

// Dashboard 返回边缘运行态势。
func (e *Engine) Dashboard() map[string]any {
	e.mu.RLock()
	defer e.mu.RUnlock()
	adapters := []Adapter{}
	for _, v := range e.adapters {
		adapters = append(adapters, *v)
	}
	points := []Point{}
	for _, v := range e.points {
		points = append(points, *v)
	}
	pending := 0
	for _, v := range e.outbox {
		if v.Status == "pending" {
			pending++
		}
	}
	return map[string]any{"metrics": map[string]any{"adapters": len(adapters), "points": len(points), "rules": len(e.rules), "pending": pending, "cloudOnline": e.cloudOnline}, "adapters": adapters, "points": points, "events": e.events, "config": e.config, "audit": e.audit}
}

// Snapshot 导出持久化状态。
func (e *Engine) Snapshot() Snapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()
	s := Snapshot{Events: e.events, Config: e.config, CloudOnline: e.cloudOnline, Audit: e.audit}
	for _, v := range e.adapters {
		s.Adapters = append(s.Adapters, *v)
	}
	for _, v := range e.points {
		s.Points = append(s.Points, *v)
	}
	for _, v := range e.rules {
		s.Rules = append(s.Rules, *v)
	}
	for _, v := range e.outbox {
		s.Outbox = append(s.Outbox, *v)
	}
	return s
}
func (e *Engine) record(action, resourceID string) {
	e.audit = append(e.audit, map[string]any{"id": makeID("aud"), "action": action, "resourceId": resourceID, "occurredAt": now()})
}
