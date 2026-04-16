// internal/vm/vm_pool.go
package vm

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dop251/goja"
	"lsengine/internal/config"
	"lsengine/internal/metrics"
)

type GojaVM struct {
	vm         *goja.Runtime
	lastUsed   time.Time
	createdAt  time.Time
	usageCount int64
	mu         sync.RWMutex
}

type GojaPool struct {
	vms   chan *GojaVM
	mu    sync.RWMutex
	stats struct {
		created int64
		reused  int64
		evicted int64
	}
	maxSize int
	minSize int
}

var GlobalGojaPool *GojaPool

func InitGojaPool() {
	GlobalGojaPool = &GojaPool{
		vms:     make(chan *GojaVM, config.MAX_VM_POOL_SIZE),
		maxSize: config.MAX_VM_POOL_SIZE,
		minSize: config.MIN_VM_POOL_SIZE,
	}

	for i := 0; i < config.MIN_VM_POOL_SIZE; i++ {
		vm := GlobalGojaPool.createVM()
		GlobalGojaPool.vms <- vm
	}

	go GlobalGojaPool.cleanup()
}

func (p *GojaPool) createVM() *GojaVM {
	vm := goja.New()
	vm.SetMaxCallStackSize(1000)

	RegisterLS(vm, config.ProjectRoot)

	return &GojaVM{
		vm:        vm,
		createdAt: time.Now(),
	}
}

func (p *GojaPool) Get() *GojaVM {
	select {
	case vm := <-p.vms:
		atomic.AddInt64(&p.stats.reused, 1)
		vm.mu.Lock()
		vm.lastUsed = time.Now()
		atomic.AddInt64(&vm.usageCount, 1)
		vm.mu.Unlock()
		return vm
	default:
		atomic.AddInt64(&p.stats.created, 1)
		return p.createVM()
	}
}

func (p *GojaPool) Put(vm *GojaVM) {
	if atomic.LoadInt64(&vm.usageCount) > 10000 {
		atomic.AddInt64(&p.stats.evicted, 1)
		return
	}

	vm.mu.Lock()
	vm.vm.ClearInterrupt()
	vm.mu.Unlock()

	select {
	case p.vms <- vm:
	default:
		atomic.AddInt64(&p.stats.evicted, 1)
	}
}

func (p *GojaPool) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		for i := 0; i < len(p.vms); i++ {
			select {
			case vm := <-p.vms:
				if time.Since(vm.lastUsed) > 30*time.Minute {
					atomic.AddInt64(&p.stats.evicted, 1)
					continue
				}
				select {
				case p.vms <- vm:
				default:
				}
			default:
				break
			}
		}
	}
}

func (p *GojaPool) Execute(code string, projectRoot string) (string, error) {
	vm := p.Get()
	defer p.Put(vm)

	timer := time.AfterFunc(5*time.Second, func() {
		vm.vm.Interrupt("execution timeout")
	})
	defer timer.Stop()

	result, err := vm.vm.RunString(code)
	if err != nil {
		return "", err
	}

	return result.String(), nil
}

func RegisterLS(vm *goja.Runtime, projectRoot string) {
	lsObj := vm.NewObject()

	// App object
	appObj := vm.NewObject()
	appObj.Set("name", config.AppCfg.Name)
	appObj.Set("version", config.AppCfg.Version)
	appObj.Set("port", config.AppCfg.Port)
	appObj.Set("root", projectRoot)
	appObj.Set("environment", config.AppCfg.Environment)
	lsObj.Set("app", appObj)

	// Console
	consoleObj := vm.NewObject()
	consoleObj.Set("log", func(call goja.FunctionCall) goja.Value {
		msg := ""
		for i, arg := range call.Arguments {
			if i > 0 {
				msg += " "
			}
			msg += arg.String()
		}
		log.Printf("[LS] %s", msg)
		return goja.Null()
	})
	vm.Set("console", consoleObj)
	vm.Set("print", consoleObj.Get("log"))

	// HTTP client
	httpClientObj := vm.NewObject()
	httpClient := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: false,
			},
		},
	}

	httpClientObj.Set("get", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			return vm.ToValue(map[string]interface{}{})
		}
		url := call.Argument(0).String()
		resp, err := httpClient.Get(url)
		if err != nil {
			return vm.ToValue(map[string]interface{}{"error": err.Error()})
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
		if err != nil {
			return vm.ToValue(map[string]interface{}{"error": err.Error()})
		}
		return vm.ToValue(map[string]interface{}{
			"status":  resp.StatusCode,
			"body":    string(body),
			"headers": resp.Header,
		})
	})
	lsObj.Set("http", httpClientObj)

	// Crypto
	cryptoObj := vm.NewObject()
	cryptoObj.Set("hash", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			return vm.ToValue("")
		}
		s := call.Argument(0).String()
		h := sha256.Sum256([]byte(s))
		return vm.ToValue(hex.EncodeToString(h[:]))
	})
	cryptoObj.Set("uuid", func(call goja.FunctionCall) goja.Value {
		b := make([]byte, 16)
		for i := range b {
			b[i] = byte(time.Now().UnixNano() >> (i * 8))
		}
		b[6] = (b[6] & 0x0f) | 0x40
		b[8] = (b[8] & 0x3f) | 0x80
		return vm.ToValue(fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]))
	})
	cryptoObj.Set("base64Encode", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			return vm.ToValue("")
		}
		return vm.ToValue(base64.StdEncoding.EncodeToString([]byte(call.Argument(0).String())))
	})
	cryptoObj.Set("base64Decode", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			return vm.ToValue("")
		}
		data, err := base64.StdEncoding.DecodeString(call.Argument(0).String())
		if err != nil {
			return vm.ToValue("")
		}
		return vm.ToValue(string(data))
	})
	lsObj.Set("crypto", cryptoObj)

	// Utils
	lsObj.Set("now", func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(time.Now().Unix())
	})
	lsObj.Set("time", func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(time.Now().Format("2006-01-02 15:04:05"))
	})
	lsObj.Set("sleep", func(call goja.FunctionCall) goja.Value {
		ms := int64(100)
		if len(call.Arguments) >= 1 {
			ms = call.Argument(0).ToInteger()
		}
		time.Sleep(time.Duration(ms) * time.Millisecond)
		return goja.Null()
	})
	lsObj.Set("json", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			return goja.Null()
		}
		var v interface{}
		err := json.Unmarshal([]byte(call.Argument(0).String()), &v)
		if err != nil {
			return goja.Null()
		}
		return vm.ToValue(v)
	})
	lsObj.Set("stringify", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			return vm.ToValue("")
		}
		jsonStr, _ := json.Marshal(call.Argument(0).Export())
		return vm.ToValue(string(jsonStr))
	})
	lsObj.Set("stats", func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(metrics.GlobalMetrics.GetStats())
	})

	vm.Set("ls", lsObj)
}