package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	coreupdater "github.com/EquentR/agent_runtime/core/updater"
	"github.com/gin-gonic/gin"
)

type maintenanceTestRegister struct{}

func (maintenanceTestRegister) Register(group *gin.RouterGroup) {
	group.GET("/resource", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	group.POST("/resource", func(c *gin.Context) { c.Status(http.StatusNoContent) })
}

func TestMaintenanceMiddlewareBlocksWritesButKeepsReadsAvailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gate := coreupdater.NewMaintenanceGate()
	if err := gate.Enter("op-1", time.Now()); err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	InitRouter(engine, []Register{maintenanceTestRegister{}}, "/api/v1", nil, maintenanceMiddleware(gate))

	read := httptest.NewRecorder()
	engine.ServeHTTP(read, httptest.NewRequest(http.MethodGet, "/api/v1/resource", nil))
	if read.Code != http.StatusNoContent {
		t.Fatalf("GET status = %d, want %d", read.Code, http.StatusNoContent)
	}

	write := httptest.NewRecorder()
	engine.ServeHTTP(write, httptest.NewRequest(http.MethodPost, "/api/v1/resource", nil))
	if write.Code != http.StatusServiceUnavailable {
		t.Fatalf("POST status = %d, want %d", write.Code, http.StatusServiceUnavailable)
	}
	var result struct {
		Code int `json:"code"`
		Data struct {
			ErrorCode string `json:"error_code"`
		} `json:"data"`
	}
	if err := json.Unmarshal(write.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.Code != http.StatusServiceUnavailable || result.Data.ErrorCode != "maintenance_mode" {
		t.Fatalf("response = %#v", result)
	}
}

func TestMaintenanceMiddlewareAllowsWritesWhenInactive(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	InitRouter(engine, []Register{maintenanceTestRegister{}}, "/api/v1", nil, maintenanceMiddleware(coreupdater.NewMaintenanceGate()))
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/resource", nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("POST status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
}
