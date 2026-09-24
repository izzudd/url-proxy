package database

import (
	"context"
	"fmt"
	"os"
	"testing"
)

func BenchmarkDatabaseGetFile(b *testing.B) {
	dbPath := "bench_get.db"
	defer os.Remove(dbPath)

	db, err := Open(dbPath)
	if err != nil {
		b.Fatalf("open db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	_ = db.UpsertFile(ctx, &FileRecord{
		ID:          "bench123",
		OriginalURL: "https://example.com/file.bin",
		Filename:    "file.bin",
		ContentType: "application/octet-stream",
		FileSize:    1048576,
	})

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = db.GetFile(ctx, "bench123")
	}
}

func BenchmarkDatabaseUpsertFile(b *testing.B) {
	dbPath := "bench_upsert.db"
	defer os.Remove(dbPath)

	db, err := Open(dbPath)
	if err != nil {
		b.Fatalf("open db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		id := fmt.Sprintf("id_%d", i)
		_ = db.UpsertFile(ctx, &FileRecord{
			ID:          id,
			OriginalURL: fmt.Sprintf("https://example.com/file_%d.bin", i),
			Filename:    "file.bin",
			ContentType: "application/octet-stream",
			FileSize:    1048576,
		})
	}
}
