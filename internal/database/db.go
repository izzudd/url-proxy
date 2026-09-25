package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type FileRecord struct {
	ID           string     `json:"id"`
	OriginalURL  string     `json:"original_url"`
	Filename     string     `json:"filename"`
	ContentType  string     `json:"content_type"`
	FileSize     int64      `json:"file_size"`
	CreatedAt    time.Time  `json:"created_at"`
	LastAccessed *time.Time `json:"last_accessed_at,omitempty"`
	AccessCount  int64      `json:"access_count"`
	BytesServed  int64      `json:"bytes_served"`
}

type Stats struct {
	TotalFiles       int64 `json:"total_files"`
	TotalBytesServed int64 `json:"total_bytes_served"`
	TotalAccessCount int64 `json:"total_access_count"`
}

type DB struct {
	*sql.DB
}

func Open(path string) (*DB, error) {
	// Treble memory optimization pragma string
	dsn := fmt.Sprintf("%s?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=cache_size(-64000)&_pragma=temp_store(MEMORY)&_pragma=mmap_size(268435456)&_pragma=busy_timeout(5000)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Single writer connection pool for SQLite to prevent write locks
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	schema := `
	CREATE TABLE IF NOT EXISTS files (
		id TEXT PRIMARY KEY,
		original_url TEXT NOT NULL UNIQUE,
		filename TEXT NOT NULL,
		content_type TEXT NOT NULL DEFAULT 'application/octet-stream',
		file_size INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		last_accessed_at DATETIME,
		access_count INTEGER NOT NULL DEFAULT 0,
		bytes_served INTEGER NOT NULL DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_files_created ON files(created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_files_url ON files(original_url);

	CREATE TABLE IF NOT EXISTS request_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		recorded_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		status_code INTEGER NOT NULL,
		bytes_served INTEGER NOT NULL DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_req_events_time ON request_events(recorded_at DESC);
	`
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("exec schema: %w", err)
	}

	return &DB{db}, nil
}

func (d *DB) UpsertFile(ctx context.Context, f *FileRecord) error {
	query := `
	INSERT INTO files (id, original_url, filename, content_type, file_size, created_at, access_count, bytes_served)
	VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP, 0, 0)
	ON CONFLICT(id) DO UPDATE SET
		filename = excluded.filename,
		content_type = excluded.content_type,
		file_size = excluded.file_size
	`
	_, err := d.ExecContext(ctx, query, f.ID, f.OriginalURL, f.Filename, f.ContentType, f.FileSize)
	return err
}

func (d *DB) GetFile(ctx context.Context, id string) (*FileRecord, error) {
	row := d.QueryRowContext(ctx, `SELECT id, original_url, filename, content_type, file_size, created_at, last_accessed_at, access_count, bytes_served FROM files WHERE id = ?`, id)
	var f FileRecord
	err := row.Scan(&f.ID, &f.OriginalURL, &f.Filename, &f.ContentType, &f.FileSize, &f.CreatedAt, &f.LastAccessed, &f.AccessCount, &f.BytesServed)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func (d *DB) RecordAccess(ctx context.Context, id string, bytesServed int64) error {
	query := `
	UPDATE files
	SET access_count = access_count + 1,
	    bytes_served = bytes_served + ?,
	    last_accessed_at = CURRENT_TIMESTAMP
	WHERE id = ?
	`
	_, err := d.ExecContext(ctx, query, bytesServed, id)
	return err
}

func (d *DB) GetStats(ctx context.Context) (Stats, error) {
	var s Stats
	query := `SELECT COUNT(*), COALESCE(SUM(bytes_served), 0), COALESCE(SUM(access_count), 0) FROM files`
	err := d.QueryRowContext(ctx, query).Scan(&s.TotalFiles, &s.TotalBytesServed, &s.TotalAccessCount)
	return s, err
}

func (d *DB) ListFiles(ctx context.Context, limit, offset int, search string) ([]FileRecord, error) {
	var rows *sql.Rows
	var err error
	if search != "" {
		pattern := "%" + search + "%"
		rows, err = d.QueryContext(ctx, `
			SELECT id, original_url, filename, content_type, file_size, created_at, last_accessed_at, access_count, bytes_served
			FROM files
			WHERE filename LIKE ? OR original_url LIKE ?
			ORDER BY created_at DESC
			LIMIT ? OFFSET ?`, pattern, pattern, limit, offset)
	} else {
		rows, err = d.QueryContext(ctx, `
			SELECT id, original_url, filename, content_type, file_size, created_at, last_accessed_at, access_count, bytes_served
			FROM files
			ORDER BY created_at DESC
			LIMIT ? OFFSET ?`, limit, offset)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []FileRecord
	for rows.Next() {
		var f FileRecord
		if err := rows.Scan(&f.ID, &f.OriginalURL, &f.Filename, &f.ContentType, &f.FileSize, &f.CreatedAt, &f.LastAccessed, &f.AccessCount, &f.BytesServed); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

func (d *DB) CountFiles(ctx context.Context, search string) (int64, error) {
	var count int64
	if search != "" {
		pattern := "%" + search + "%"
		err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM files WHERE filename LIKE ? OR original_url LIKE ?`, pattern, pattern).Scan(&count)
		return count, err
	}
	err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM files`).Scan(&count)
	return count, err
}

type ChartData struct {
	Labels  []string `json:"labels"`
	Success []int64  `json:"success"`
	Error   []int64  `json:"error"`
}

func (d *DB) RecordRequestEvent(ctx context.Context, statusCode int, bytesServed int64) error {
	query := `INSERT INTO request_events (status_code, bytes_served) VALUES (?, ?)`
	_, err := d.ExecContext(ctx, query, statusCode, bytesServed)
	return err
}

func (d *DB) GetChartMetrics(ctx context.Context) (ChartData, error) {
	query := `
	SELECT 
		strftime('%H:00', recorded_at) AS hr,
		SUM(CASE WHEN status_code < 400 THEN 1 ELSE 0 END) AS success_cnt,
		SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END) AS error_cnt
	FROM request_events
	WHERE recorded_at >= datetime('now', '-24 hours')
	GROUP BY hr
	ORDER BY recorded_at ASC
	`
	rows, err := d.QueryContext(ctx, query)
	if err != nil {
		return ChartData{}, err
	}
	defer rows.Close()

	var data ChartData
	for rows.Next() {
		var hr string
		var succ, errCount int64
		if err := rows.Scan(&hr, &succ, &errCount); err != nil {
			return ChartData{}, err
		}
		data.Labels = append(data.Labels, hr)
		data.Success = append(data.Success, succ)
		data.Error = append(data.Error, errCount)
	}

	if len(data.Labels) == 0 {
		data.Labels = []string{"Now"}
		data.Success = []int64{0}
		data.Error = []int64{0}
	}

	return data, rows.Err()
}
