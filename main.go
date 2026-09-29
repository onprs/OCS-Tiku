package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
)

func main() {
	dataDir := flag.String("data-dir", ".", "配置、数据库和图片目录")
	frontendDir := flag.String("frontend-dir", "frontend/dist", "前端构建目录")
	listen := flag.String("listen", "", "监听地址（覆盖配置文件）")
	flag.Parse()

	config, err := loadConfig(filepath.Join(*dataDir, "config.yaml"))
	if err != nil {
		log.Fatal(err)
	}
	cfg := config.snapshot()
	address := *listen
	if address == "" {
		address = fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	}
	if _, _, err := net.SplitHostPort(address); err != nil {
		log.Fatalf("监听地址无效: %v", err)
	}
	if _, err := os.Stat(filepath.Join(*frontendDir, "index.html")); err != nil {
		log.Fatalf("前端未构建: 请先在 frontend 目录运行 npm ci 和 npm run build: %v", err)
	}
	store, err := openStore(filepath.Join(*dataDir, "ocs-tiku.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer store.close()
	images, err := newImageProcessor(filepath.Join(*dataDir, "images"))
	if err != nil {
		log.Fatal(err)
	}
	models := newModelClient()
	server := &Server{config: config, store: store, models: models, solver: &Solver{config: config, models: models, images: images},
		assets: *frontendDir, imageDir: images.dir, address: address}
	log.Printf("OCS 题库服务: http://%s", address)
	log.Fatal(http.ListenAndServe(address, server.handler()))
}
