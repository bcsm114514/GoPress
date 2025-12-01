package plugins

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"github.com/dop251/goja"
	"github.com/gofiber/fiber/v2"
	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
)

// === 结构体定义 ===

type PluginSetting struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Type        string   `json:"type"`
	Value       string   `json:"value"`
	Default     string   `json:"default"`
	Options     []string `json:"options"`
	Description string   `json:"description"`
}

type PluginMetadata struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Version     string          `json:"version"`
	Description string          `json:"description"`
	Author      string          `json:"author"`
	Entry       string          `json:"entry"`
	Active      bool            `json:"active"`
	Settings    []PluginSetting `json:"settings"`
	DirName     string          `json:"-"`
}

type RouteDef struct {
	Method      string
	Path        string
	HandlerName string
}

type PluginInstance struct {
	Meta   PluginMetadata
	Config map[string]string // 用户配置
	Routes []RouteDef
	Hooks  map[string]func(string) string
	JsVM   *goja.Runtime
	GoInt  *interp.Interpreter
}

var (
	mu        sync.RWMutex
	Instances = make(map[string]*PluginInstance)
)

// === 初始化逻辑 ===

func Init() {
	newInstances := make(map[string]*PluginInstance)
	entries, err := os.ReadDir("./plugins")
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				if instance := loadOne(entry.Name()); instance != nil {
					newInstances[instance.Meta.ID] = instance
				}
			}
		}
	}
	mu.Lock()
	Instances = newInstances
	mu.Unlock()
	log.Printf("插件系统重载完成，加载插件数: %d", len(newInstances))
}

func loadOne(dirName string) *PluginInstance {
	basePath := "./plugins/" + dirName
	data, err := os.ReadFile(basePath + "/plugin.json")
	if err != nil {
		return nil
	}

	var meta PluginMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil
	}
	meta.DirName = dirName

	// 解析配置
	configMap := make(map[string]string)
	for _, s := range meta.Settings {
		val := s.Value
		if val == "" {
			val = s.Default
		}
		configMap[s.Key] = val
	}

	instance := &PluginInstance{
		Meta:   meta,
		Config: configMap,
		Hooks:  make(map[string]func(string) string),
		Routes: []RouteDef{},
	}

	if !meta.Active {
		return instance
	}

	entryPath := filepath.Join(basePath, meta.Entry)
	code, err := os.ReadFile(entryPath)
	if err != nil {
		return instance
	}

	src := string(code)
	ext := strings.ToLower(filepath.Ext(meta.Entry))

	if ext == ".js" {
		loadJS(instance, src)
	} else if ext == ".go" {
		loadGo(instance, src)
	}

	return instance
}

// === 钩子调用 (OnContentRender / OnMarkdown) ===
func ApplyFilter(hookName string, content string) string {
	mu.RLock()
	defer mu.RUnlock()
	for _, p := range Instances {
		if p.Meta.Active && p.Hooks != nil {
			if hook, exists := p.Hooks[hookName]; exists {
				content = hook(content)
			}
		}
	}
	return content
}

// === 请求生命周期钩子 (OnRequest) ===
// 返回 (响应内容, 是否拦截)
func ApplyRequestFilter(url string) (string, bool) {
	mu.RLock()
	defer mu.RUnlock()
	for _, p := range Instances {
		if p.Meta.Active && p.Hooks != nil {
			if hook, exists := p.Hooks["OnRequest"]; exists {
				res := hook(url)
				if res != "" {
					return res, true
				}
			}
		}
	}
	return "", false
}

// === 响应生命周期钩子 (OnResponse) ===
// 将 url 和 html 序列化为 JSON 传给插件
func ApplyResponseFilter(url string, html string) {
	mu.RLock()
	defer mu.RUnlock()

	payload, _ := json.Marshal(map[string]string{"url": url, "html": html})
	strPayload := string(payload)

	for _, p := range Instances {
		if p.Meta.Active && p.Hooks != nil {
			if hook, exists := p.Hooks["OnResponse"]; exists {
				hook(strPayload)
			}
		}
	}
}

// === 路由匹配 ===
func MatchRoute(method, path string) (interface{}, bool) {
	mu.RLock()
	defer mu.RUnlock()
	for _, p := range Instances {
		if !p.Meta.Active {
			continue
		}
		for _, r := range p.Routes {
			if strings.EqualFold(r.Method, method) && r.Path == path {
				if p.JsVM != nil {
					val := p.JsVM.Get(r.HandlerName)
					if fn, ok := goja.AssertFunction(val); ok {
						if res, err := fn(goja.Undefined(), p.JsVM.ToValue(path)); err == nil {
							return res.Export(), true
						}
					}
				}
				if p.GoInt != nil {
					v, err := p.GoInt.Eval(r.HandlerName)
					if err == nil && v.Kind() == reflect.Func {
						res := v.Call([]reflect.Value{reflect.ValueOf(path)})
						if len(res) > 0 {
							return res[0].Interface(), true
						}
					}
				}
			}
		}
	}
	return nil, false
}

// === 辅助方法 ===
func GetAllPlugins() []PluginMetadata {
	mu.RLock()
	defer mu.RUnlock()
	var list []PluginMetadata
	for _, p := range Instances {
		list = append(list, p.Meta)
	}
	return list
}

func GetInstance(id string) (*PluginInstance, bool) {
	mu.RLock()
	defer mu.RUnlock()
	p, ok := Instances[id]
	return p, ok
}

// === JS 引擎 ===
func loadJS(p *PluginInstance, src string) {
	vm := goja.New()
	p.JsVM = vm

	vm.Set("PluginConfig", p.Config)
	vm.Set("console", map[string]interface{}{
		"log": func(call goja.FunctionCall) goja.Value {
			log.Printf("[JS:%s] %s", p.Meta.Name, call.Argument(0).String())
			return goja.Undefined()
		},
	})
	vm.Set("RegisterRoute", func(call goja.FunctionCall) goja.Value {
		p.Routes = append(p.Routes, RouteDef{
			Method: call.Argument(0).String(), Path: call.Argument(1).String(), HandlerName: call.Argument(2).String(),
		})
		return goja.Undefined()
	})

	_, err := vm.RunString(src)
	if err != nil {
		log.Printf("JS Error [%s]: %v", p.Meta.Name, err)
		return
	}

	for _, h := range []string{"OnContentRender", "OnMarkdown", "OnRequest", "OnResponse"} {
		registerJSHook(vm, p, h)
	}
}

func registerJSHook(vm *goja.Runtime, p *PluginInstance, hookName string) {
	if fn, ok := goja.AssertFunction(vm.Get(hookName)); ok {
		p.Hooks[hookName] = func(in string) string {
			res, _ := fn(goja.Undefined(), vm.ToValue(in))
			return res.String()
		}
	}
}

// === Go 引擎 (Yaegi) ===
func loadGo(p *PluginInstance, src string) {
	i := interp.New(interp.Options{})
	i.Use(stdlib.Symbols) // 允许使用 net, json, time 等标准库！
	p.GoInt = i

	// 注入 Host API
	i.Use(interp.Exports{
		"plugin/plugin": {
			"RegisterRoute": reflect.ValueOf(func(method, path, handler string) {
				p.Routes = append(p.Routes, RouteDef{Method: method, Path: path, HandlerName: handler})
			}),
			"Log": reflect.ValueOf(func(msg string) {
				log.Printf("[Go:%s] %s", p.Meta.Name, msg)
			}),
			// [新增] 允许 Go 脚本获取配置
			"GetConfig": reflect.ValueOf(func() map[string]string {
				return p.Config
			}),
		},
	})

	_, err := i.Eval(src)
	if err != nil {
		log.Printf("Go Error [%s]: %v", p.Meta.Name, err)
		return
	}

	for _, h := range []string{"OnContentRender", "OnMarkdown", "OnRequest", "OnResponse"} {
		registerGoHook(i, p, h)
	}
}

func registerGoHook(i *interp.Interpreter, p *PluginInstance, hookName string) {
	if v, err := i.Eval(hookName); err == nil && v.Kind() == reflect.Func {
		p.Hooks[hookName] = func(in string) string {
			res := v.Call([]reflect.Value{reflect.ValueOf(in)})
			if len(res) > 0 {
				return res[0].String()
			}
			return in
		}
	}
}

func GetActiveRoutes() []string {
	mu.RLock()
	defer mu.RUnlock()

	var paths []string
	for _, p := range Instances {
		// 只处理已启用的插件
		if !p.Meta.Active {
			continue
		}

		for _, r := range p.Routes {
			// 只收录 GET 请求，且排除带参数的动态路由(如 /post/:id)
			if strings.ToUpper(r.Method) == "GET" && !strings.Contains(r.Path, ":") {
				paths = append(paths, r.Path)
			}
		}
	}
	return paths
}

func InitRoutes(app *fiber.App, handler func(*fiber.Ctx, string, map[string]interface{}) error) {}
