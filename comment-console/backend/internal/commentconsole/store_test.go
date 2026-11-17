package commentconsole

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
)

func TestMySQLConnectionErrorsHideCredentials(t *testing.T) {
	for _, dsn := range []string{
		"user:private-password@invalid-dsn",
		"user:private-password@tcp(127.0.0.1:1)/test?timeout=20ms",
	} {
		store, _, err := OpenStore(dsn)
		if store != nil {
			store.Close()
		}
		if err == nil {
			t.Fatal("无效连接应报错")
		}
		if strings.Contains(err.Error(), "private-password") || strings.Contains(err.Error(), dsn) {
			t.Fatalf("连接错误泄露凭证: %v", err)
		}
	}
	err := mysqlError("保存", &mysql.MySQLError{Number: 1064, Message: "raw SQL containing private-password"})
	if strings.Contains(err.Error(), "private-password") {
		t.Fatal("服务端错误泄露查询数据")
	}
}

// TEST_MYSQL_DSN 必须指向专用测试库；测试结束恢复原始工作空间快照。
func TestMySQLStoreIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("未设置 TEST_MYSQL_DSN，跳过真实 MySQL 集成测试")
	}
	store, original, err := OpenStore(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	t.Cleanup(func() {
		fresh, _, openErr := OpenStore(dsn)
		if openErr != nil {
			t.Errorf("恢复测试库连接失败: %v", openErr)
			return
		}
		defer fresh.Close()
		if saveErr := fresh.Save(original); saveErr != nil {
			t.Errorf("恢复测试库快照失败: %v", saveErr)
		}
	})
	stale, _, err := OpenStore(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer stale.Close()
	state := clone(original)
	state.Materials = append(state.Materials, Material{ID: "mysql-integration", Name: "中文与表情 🎉", Content: "装修预算怎么分配？💬", Enabled: true})
	if err = store.Save(state); err != nil {
		t.Fatal(err)
	}
	if err = stale.Save(original); !errors.Is(err, ErrStoreConflict) {
		t.Fatalf("并发旧版本未被CAS拒绝: %v", err)
	}
	reopened, persisted, err := OpenStore(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if len(persisted.Materials) != len(state.Materials) {
		t.Fatal("重开连接后数据缺失")
	}
	last := persisted.Materials[len(persisted.Materials)-1]
	if last.Name != "中文与表情 🎉" || last.Content != "装修预算怎么分配？💬" {
		t.Fatal("中文或 emoji 未完整持久化")
	}
	var charset string
	if err = reopened.db.QueryRowContext(context.Background(), "SELECT @@character_set_connection").Scan(&charset); err != nil {
		t.Fatal(mysqlError("读取测试字符集", err))
	}
	if charset != "utf8mb4" {
		t.Fatalf("连接字符集=%s，期望utf8mb4", charset)
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal("测试DSN无效")
	}
	cfg.User = "comment_console_invalid_test_user"
	cfg.Passwd = "private-wrong-password"
	if s, _, connectErr := OpenStore(cfg.FormatDSN()); connectErr == nil {
		s.Close()
		t.Fatal("错误账号应连接失败")
	} else if strings.Contains(connectErr.Error(), cfg.Passwd) {
		t.Fatal("真实MySQL认证错误泄露密码")
	}
}
