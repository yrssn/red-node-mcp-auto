package server

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/go-sql-driver/mysql"
)

type consoleMySQLConfig struct {
	DSN     string `json:"dsn"`
	DemoDSN string `json:"demo_dsn"`
}

func loadConsoleDSN(path string, demo bool) (string, error) {
	var cfg consoleMySQLConfig
	prodEnv := strings.TrimSpace(os.Getenv("MYSQL_DSN"))
	demoEnv := strings.TrimSpace(os.Getenv("MYSQL_DEMO_DSN"))
	selectedEnv := prodEnv
	if demo {
		selectedEnv = demoEnv
	}
	data, err := os.ReadFile(path)
	if err == nil {
		if json.Unmarshal(data, &cfg) != nil {
			if selectedEnv == "" {
				return "", fmt.Errorf("MySQL 配置文件格式错误，请参照 config/mysql.example.json")
			}
			cfg = consoleMySQLConfig{}
		}
	} else if !os.IsNotExist(err) && selectedEnv == "" {
		return "", fmt.Errorf("无法读取 MySQL 配置文件，请检查路径与访问权限")
	}
	if prodEnv != "" {
		cfg.DSN = prodEnv
	}
	if demoEnv != "" {
		cfg.DemoDSN = demoEnv
	}
	selected, varName := cfg.DSN, "MYSQL_DSN"
	if demo {
		selected, varName = cfg.DemoDSN, "MYSQL_DEMO_DSN"
	}
	if strings.TrimSpace(selected) == "" {
		return "", fmt.Errorf("未配置 MySQL：请填写 %s 或 %s 中对应的 DSN", varName, path)
	}
	parsed, err := mysql.ParseDSN(selected)
	if err != nil || parsed.DBName == "" {
		return "", fmt.Errorf("MySQL DSN 无效，请检查连接格式并指定数据库")
	}
	if demo && cfg.DSN != "" {
		prod, err := mysql.ParseDSN(cfg.DSN)
		if err != nil {
			return "", fmt.Errorf("正式 MySQL DSN 无效，请检查本地配置")
		}
		if sameMySQLDatabase(prod, parsed) {
			return "", fmt.Errorf("演示与正式模式必须使用不同数据库")
		}
	}
	return selected, nil
}

func sameMySQLDatabase(a, b *mysql.Config) bool {
	address := func(cfg *mysql.Config) string {
		if cfg.Net != "tcp" {
			return cfg.Net + ":" + cfg.Addr
		}
		host, port, err := net.SplitHostPort(cfg.Addr)
		if err != nil {
			return strings.ToLower(cfg.Addr)
		}
		if strings.EqualFold(host, "localhost") {
			host = "127.0.0.1"
		}
		return net.JoinHostPort(strings.ToLower(host), port)
	}
	return address(a) == address(b) && a.DBName == b.DBName
}
