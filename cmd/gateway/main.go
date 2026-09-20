// 知华边缘网关本地管理服务。
// 上海如静知华信息科技有限公司：https://www.zhuatech.cn/
// 商业授权或定制开发请微信添加微信号zhuatech或zhuatech2进行咨询。
package main

import (
	"cn.zhuatech/edge-gateway/internal/gateway"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type app struct {
	e         *gateway.Engine
	file, key string
}

func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("content-type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func (a *app) save() {
	_ = os.MkdirAll(filepath.Dir(a.file), 0755)
	b, _ := json.MarshalIndent(a.e.Snapshot(), "", "  ")
	_ = os.WriteFile(a.file, b, 0600)
}
func (a *app) api(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("x-api-key") != a.key {
		write(w, 401, map[string]string{"error": "UNAUTHORIZED"})
		return
	}
	if r.Method == "GET" && r.URL.Path == "/api/dashboard" {
		write(w, 200, a.e.Dashboard())
		return
	}
	if r.Method == "POST" && r.URL.Path == "/api/adapters" {
		var x struct{ Name, Protocol, Endpoint string }
		_ = json.NewDecoder(r.Body).Decode(&x)
		adapter, err := a.e.AddAdapter(x.Name, x.Protocol, x.Endpoint)
		if err != nil {
			write(w, 400, map[string]string{"error": err.Error()})
			return
		}
		a.save()
		write(w, 201, adapter)
		return
	}
	if r.Method == "POST" && r.URL.Path == "/api/points" {
		var x struct {
			AdapterID, Code, Address, Unit string
			Scale                          float64
		}
		_ = json.NewDecoder(r.Body).Decode(&x)
		point, err := a.e.AddPoint(x.AdapterID, x.Code, x.Address, x.Unit, x.Scale)
		if err != nil {
			write(w, 400, map[string]string{"error": err.Error()})
			return
		}
		a.save()
		write(w, 201, point)
		return
	}
	if r.Method == "POST" && r.URL.Path == "/api/rules" {
		var x struct {
			Name, PointCode, Operator, Action string
			Threshold                         float64
		}
		_ = json.NewDecoder(r.Body).Decode(&x)
		rule, err := a.e.AddRule(x.Name, x.PointCode, x.Operator, x.Threshold, x.Action)
		if err != nil {
			write(w, 400, map[string]string{"error": err.Error()})
			return
		}
		a.save()
		write(w, 201, rule)
		return
	}
	if r.Method == "POST" && r.URL.Path == "/api/config" {
		var x struct {
			Revision              int64
			Body, Checksum, Actor string
		}
		_ = json.NewDecoder(r.Body).Decode(&x)
		config, err := a.e.ApplyConfig(x.Revision, x.Body, strings.ToLower(x.Checksum), x.Actor)
		if err != nil {
			write(w, 400, map[string]string{"error": err.Error()})
			return
		}
		a.save()
		write(w, 200, config)
		return
	}
	if r.Method == "POST" && r.URL.Path == "/api/ingest" {
		var x struct {
			PointCode string
			Raw       float64
			Quality   string
		}
		_ = json.NewDecoder(r.Body).Decode(&x)
		p, events, err := a.e.Ingest(x.PointCode, x.Raw, x.Quality)
		if err != nil {
			write(w, 400, map[string]string{"error": err.Error()})
			return
		}
		a.save()
		write(w, 202, map[string]any{"point": p, "events": events})
		return
	}
	if r.Method == "POST" && r.URL.Path == "/api/cloud-link" {
		var x struct{ Online bool }
		_ = json.NewDecoder(r.Body).Decode(&x)
		a.e.SetCloudOnline(x.Online)
		a.save()
		write(w, 200, map[string]bool{"online": x.Online})
		return
	}
	if r.Method == "POST" && r.URL.Path == "/api/outbox/drain" {
		batch, err := a.e.PendingBatch(100)
		if err != nil {
			write(w, 409, map[string]string{"error": err.Error()})
			return
		}
		ids := []string{}
		for _, x := range batch {
			ids = append(ids, x.ID)
		}
		a.e.Ack(ids)
		a.save()
		write(w, 200, map[string]any{"delivered": len(ids), "items": batch})
		return
	}
	write(w, 404, map[string]string{"error": "NOT_FOUND"})
}
func main() {
	file := env("EDGE_DATA_FILE", "./data/gateway.json")
	s := gateway.Snapshot{}
	if b, err := os.ReadFile(file); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	a := &app{e: gateway.NewEngine(s), file: file, key: env("EDGE_API_KEY", "zhuatech-demo-key")}
	if len(a.e.Snapshot().Adapters) == 0 {
		ad, _, _ := seed(a.e)
		_ = ad
	}
	a.save()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { write(w, 200, map[string]string{"status": "UP"}) })
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/api/", a.api)
	mux.Handle("/", http.FileServer(http.Dir("web")))
	port := env("PORT", "18083")
	log.Printf("ZhuaTech Edge Gateway running at http://127.0.0.1:%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
func seed(e *gateway.Engine) (gateway.Adapter, gateway.Point, error) {
	a, err := e.AddAdapter("一号产线采集器", "simulator", "line://packing-01")
	if err != nil {
		return gateway.Adapter{}, gateway.Point{}, err
	}
	p, err := e.AddPoint(a.ID, "packing.motor.temperature", "40001", "℃", .1)
	if err != nil {
		return a, gateway.Point{}, err
	}
	_, _ = e.AddRule("电机高温", "packing.motor.temperature", "gt", 80, "raise_alarm_and_stop")
	e.SetCloudOnline(false)
	_, _, _ = e.Ingest(p.Code, 768, "good")
	body := "{\"pollSeconds\":5}"
	sum := sha256.Sum256([]byte(body))
	_, _ = e.ApplyConfig(1, body, hex.EncodeToString(sum[:]), "system")
	return a, p, nil
}
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
