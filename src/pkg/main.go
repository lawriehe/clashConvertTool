package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
)

// VmessNode 用于解析 vmess:// 链接解码后的 JSON
type VmessNode struct {
	Add  string `json:"add"`  // 地址
	Aid  int    `json:"aid"`  // alterId
	Host string `json:"host"` // 伪装域名
	ID   string `json:"id"`   // UUID
	Net  string `json:"net"`  // 网络类型 (ws, tcp)
	Path string `json:"path"` // WebSocket 路径
	Port string `json:"port"` // 端口
	PS   string `json:"ps"`   // 节点名称 (Remark)
	TLS  string `json:"tls"`  // 是否启用 TLS
	Type string `json:"type"` // 伪装类型 (none, http)
	V    string `json:"v"`    // 版本
}

// ClashProxy 代表 Clash 配置中的一个代理项
type ClashProxy struct {
	Name     string                 `yaml:"name"`
	Type     string                 `yaml:"type"`
	Server   string                 `yaml:"server"`
	Port     int                    `yaml:"port"`
	UUID     string                 `yaml:"uuid"`
	AlterID  int                    `yaml:"alterId"`
	Cipher   string                 `yaml:"cipher"`
	TLS      bool                   `yaml:"tls"`
	Network  string                 `yaml:"network,omitempty"`
	WSOpts   map[string]interface{} `yaml:"ws-opts,omitempty"`
	SkipCert bool                   `yaml:"skip-cert-verify"`
}

// RulesProvider defines the structure for rule providers
type RulesProvider struct {
	Type     string `yaml:"type"`
	Behavior string `yaml:"behavior"`
	URL      string `yaml:"url"`
	Path     string `yaml:"path"`
	Interval int    `yaml:"interval"`
}

// ClashConfig 代表完整的 Clash 配置文件结构
type ClashConfig struct {
	Port           int                      `yaml:"port"`
	SocksPort      int                      `yaml:"socks-port"`
	AllowLan       bool                     `yaml:"allow-lan"`
	Mode           string                   `yaml:"mode"`
	LogLevel       string                   `yaml:"log-level"`
	ExternalCtrl   string                   `yaml:"external-controller"`
	Proxies        []ClashProxy             `yaml:"proxies"`
	ProxyGroups    []ProxyGroup             `yaml:"proxy-groups"`
	RulesProviders map[string]RulesProvider `yaml:"rule-providers"`
	Rules          []string                 `yaml:"rules"`
}

// ProxyGroup 代表 Clash 配置中的代理组
type ProxyGroup struct {
	Name    string   `yaml:"name"`
	Type    string   `yaml:"type"`
	Proxies []string `yaml:"proxies"`
}

func main() {
	cfg, err := Init()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
		return
	}
	r := gin.New()

	// 添加中间件
	r.Use(gin.Logger())
	r.Use(gin.Recovery())

	// 添加自定义中间件（示例）
	r.Use(func(c *gin.Context) {
		c.Set("config", cfg)
		c.Next()
	})

	// 健康检查路由
	r.GET("/health", healthCheck)

	// 配置信息路由
	r.GET("/config", processConfig)
	r.Run(":8088")
}

func processConfig(c *gin.Context) {
	data, err := processConvert()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}

	c.Header("Content-Type", "application/x-yaml")
	c.Header("Content-Disposition", "attachment; filename=\"out.yaml\"")

	// 返回 YAML 流
	c.Data(http.StatusOK, "application/x-yaml", data)

}

func healthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":    "healthy",
		"timestamp": time.Now().Unix(),
		"config":    Global.Url,
	})
}

func processConvert() (data []byte, err error) {
	// 1. 获取订阅内容
	log.Println("Fetching subscription content from:", Global.Url)
	resp, err := http.Get(Global.Url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch subscription URL: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read subscription response body: %v", err)
	}

	// 2. Base64 解码
	decodedBody, err := base64.StdEncoding.DecodeString(string(body))
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64 subscription content: %v", err)
	}

	// 3. 按行分割节点链接
	nodeLinks := strings.Split(string(decodedBody), "\n")

	var clashProxies []ClashProxy
	var proxyNames []string

	// 4. 循环解析每个节点
	for _, link := range nodeLinks {
		link = strings.TrimSpace(link)
		if strings.HasPrefix(link, "vmess://") {
			vmessBase64 := strings.TrimPrefix(link, "vmess://")
			if len(vmessBase64)%4 != 0 {
				padding_needed := 4 - (len(vmessBase64) % 4)
				for i := 0; i < padding_needed; i++ {
					vmessBase64 += "="
				}
			}

			vmessJSON, err := base64.StdEncoding.DecodeString(vmessBase64)
			if err != nil {
				log.Printf("Warning: Failed to decode vmess link, skipping: %v", err)
				continue
			}

			var node VmessNode
			if err := json.Unmarshal(vmessJSON, &node); err != nil {
				log.Printf("Warning: Failed to unmarshal vmess JSON, skipping: %v", err)
				continue
			}

			// 5. 转换为 ClashProxy 结构
			proxy, err := convertVmessToClashProxy(node)
			if err != nil {
				log.Printf("Warning: Failed to convert vmess node '%s', skipping: %v", node.PS, err)
				continue
			}
			clashProxies = append(clashProxies, proxy)
			proxyNames = append(proxyNames, proxy.Name)
		}
	}

	if len(clashProxies) == 0 {
		return nil, fmt.Errorf("no valid vmess nodes found in the subscription")
	}
	log.Printf("Successfully converted %d nodes.", len(clashProxies))

	// 6. 创建完整的 Clash 配置
	clashConfig := createDefaultClashConfig(clashProxies, proxyNames)

	// 7. 序列化为 YAML
	yamlData, err := yaml.Marshal(clashConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal clash config to YAML: %v", err)
	}

	return yamlData, nil
}

// convertVmessToClashProxy 将 VmessNode 转换为 ClashProxy
func convertVmessToClashProxy(node VmessNode) (ClashProxy, error) {
	port, err := strconv.Atoi(node.Port)
	if err != nil {
		return ClashProxy{}, fmt.Errorf("invalid port: %s", node.Port)
	}

	proxy := ClashProxy{
		Name:     node.PS,
		Type:     "vmess",
		Server:   node.Add,
		Port:     port,
		UUID:     node.ID,
		AlterID:  int(node.Aid),
		Cipher:   "auto", // Clash 会自动选择
		TLS:      node.TLS == "tls",
		SkipCert: true, // 通常建议跳过证书验证
		Network:  node.Net,
	}

	if node.Net == "ws" {
		proxy.WSOpts = make(map[string]interface{})
		proxy.WSOpts["path"] = node.Path
		headers := make(map[string]string)
		headers["Host"] = node.Host
		proxy.WSOpts["headers"] = headers
	}

	return proxy, nil
}

// createDefaultClashConfig 创建一个默认的 Clash 配置框架
func createDefaultClashConfig(proxies []ClashProxy, proxyNames []string) ClashConfig {
	// Read template file
	f, err := os.ReadFile("resources/out-template.yaml")
	if err != nil {
		log.Printf("Error reading template file: %v, using hardcoded defaults", err)
		// Fallback to hardcoded defaults if template fails
		return ClashConfig{
			Port:         7890,
			SocksPort:    7891,
			AllowLan:     true,
			Mode:         "Rule",
			LogLevel:     "info",
			ExternalCtrl: "127.0.0.1:9090",
			Proxies:      proxies,
			ProxyGroups: []ProxyGroup{
				{
					Name:    "PROXY",
					Type:    "select",
					Proxies: append([]string{"DIRECT", "REJECT"}, proxyNames...),
				},
			},
			Rules: []string{
				"MATCH,DIRECT",
			},
		}
	}

	// Temporary struct for parsing template
	type TemplateConfig struct {
		Port          int                      `yaml:"port"`
		SocksPort     int                      `yaml:"socks-port"`
		AllowLan      bool                     `yaml:"allow-lan"`
		Mode          string                   `yaml:"mode"`
		LogLevel      string                   `yaml:"log-level"`
		ExternalCtrl  string                   `yaml:"external-controller"`
		RuleProviders map[string]RulesProvider `yaml:"rule-providers"`
		Rules         []string                 `yaml:"rules"`
		ProxyGroups   []map[string]interface{} `yaml:"proxy-groups"`
	}

	var tmpl TemplateConfig
	if err := yaml.Unmarshal(f, &tmpl); err != nil {
		log.Fatalf("Error parsing template: %v", err)
	}

	var proxyGroups []ProxyGroup
	for _, g := range tmpl.ProxyGroups {
		name, _ := g["name"].(string)
		typ, _ := g["type"].(string)

		var groupProxies []string
		// Check proxies field
		if p, ok := g["proxies"].(string); ok && p == "${proxies}" {
			groupProxies = append(groupProxies, proxyNames...)
		} else if pList, ok := g["proxies"].([]interface{}); ok {
			for _, pItem := range pList {
				if s, ok := pItem.(string); ok {
					groupProxies = append(groupProxies, s)
				}
			}
		}

		proxyGroups = append(proxyGroups, ProxyGroup{
			Name:    name,
			Type:    typ,
			Proxies: groupProxies,
		})
	}

	return ClashConfig{
		Port:           tmpl.Port,
		SocksPort:      tmpl.SocksPort,
		AllowLan:       tmpl.AllowLan,
		Mode:           tmpl.Mode,
		LogLevel:       tmpl.LogLevel,
		ExternalCtrl:   tmpl.ExternalCtrl,
		Proxies:        proxies,
		ProxyGroups:    proxyGroups,
		RulesProviders: tmpl.RuleProviders,
		Rules:          tmpl.Rules,
	}
}
