package commentconsole

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/go-sql-driver/mysql"
)

// StateStore 供业务层持久化状态，测试可注入内存实现。
type StateStore interface {
	Save(State) error
	Close() error
}

var ErrStoreConflict = errors.New("工作空间已被其他服务修改，请停止重复实例后重启当前服务")

const stateSchema = `CREATE TABLE IF NOT EXISTS comment_console_state (
	id TINYINT UNSIGNED NOT NULL PRIMARY KEY,
	revision BIGINT UNSIGNED NOT NULL DEFAULT 0,
	data LONGTEXT NOT NULL,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`

// Store 用 revision CAS 防止多个服务实例静默覆盖同一工作空间。
type Store struct {
	mu       sync.Mutex
	db       *sql.DB
	revision uint64
}

type silentMySQLLogger struct{}

func (silentMySQLLogger) Print(...any) {}

func EmptyState() State {
	return State{Models: []Model{}, Strategies: []Strategy{}, Materials: []Material{}, Tasks: []Task{}, Posts: []Post{}, Comments: []Comment{}, Logs: []Log{}, Settings: Settings{SendInterval: 60}}
}

// OpenStore 连接已有数据库并自动建表，不创建数据库，不输出 DSN。
func OpenStore(dsn string) (*Store, State, error) {
	var state State
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || cfg.DBName == "" {
		return nil, state, errors.New("MySQL DSN 无效，请填写连接参数和已创建的数据库名")
	}
	if err = cfg.Apply(mysql.Charset("utf8mb4", "utf8mb4_unicode_ci")); err != nil {
		return nil, state, errors.New("MySQL 字符集配置失败")
	}
	cfg.Logger = silentMySQLLogger{}
	cfg.MultiStatements = false
	cfg.InterpolateParams = false
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	if cfg.ReadTimeout <= 0 {
		cfg.ReadTimeout = 15 * time.Second
	}
	if cfg.WriteTimeout <= 0 {
		cfg.WriteTimeout = 15 * time.Second
	}
	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		return nil, state, errors.New("MySQL 连接配置无效，请检查 DSN 参数")
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(3 * time.Minute)
	s := &Store{db: db}
	fail := func(op string, cause error) (*Store, State, error) {
		_ = db.Close()
		return nil, state, mysqlError(op, cause)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		return fail("连接 MySQL", err)
	}
	if _, err = db.ExecContext(ctx, stateSchema); err != nil {
		return fail("创建控制台数据表", err)
	}
	initial, _ := json.Marshal(EmptyState())
	if _, err = db.ExecContext(ctx, "INSERT IGNORE INTO comment_console_state (id, revision, data) VALUES (1, 0, ?)", initial); err != nil {
		return fail("初始化工作空间", err)
	}
	var data []byte
	if err = db.QueryRowContext(ctx, "SELECT revision, data FROM comment_console_state WHERE id=1").Scan(&s.revision, &data); err != nil {
		return fail("读取工作空间", err)
	}
	if err = json.Unmarshal(data, &state); err != nil {
		_ = db.Close()
		return nil, state, errors.New("MySQL 工作空间数据格式无效，请检查备份")
	}
	return s, state, nil
}

func mysqlError(operation string, err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s超时，请检查网络和数据库状态", operation)
	}
	var serverError *mysql.MySQLError
	if errors.As(err, &serverError) {
		switch serverError.Number {
		case 1045:
			return fmt.Errorf("%s失败：账号或密码不正确", operation)
		case 1049:
			return fmt.Errorf("%s失败：数据库不存在，请先创建数据库", operation)
		case 1044, 1142:
			return fmt.Errorf("%s失败：数据库权限不足", operation)
		default:
			return fmt.Errorf("%s失败（MySQL 错误码 %d）", operation, serverError.Number)
		}
	}
	return fmt.Errorf("%s失败，请检查连接配置、网络和数据库状态", operation)
}

// Save 只有 revision 仍与读取时一致才能提交；失败时不更新本地版本。
func (s *Store) Save(state State) error {
	data, err := json.Marshal(state)
	if err != nil {
		return errors.New("工作空间数据无法编码")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result, err := s.db.ExecContext(ctx, "UPDATE comment_console_state SET data=?, revision=revision+1 WHERE id=1 AND revision=?", data, s.revision)
	if err != nil {
		return mysqlError("保存工作空间", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return mysqlError("确认工作空间保存结果", err)
	}
	if rows != 1 {
		return ErrStoreConflict
	}
	s.revision++
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

func clone(s State) State {
	b, _ := json.Marshal(s)
	var out State
	_ = json.Unmarshal(b, &out)
	return out
}
