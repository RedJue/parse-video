package main

import (
	"context"
	"embed"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/wujunwei928/parse-video/parser"
)

type HttpResponse struct {
	Code int         `json:"code"`
	Msg  string      `json:"msg"`
	Data interface{} `json:"data"`
}

//go:embed templates/*
var files embed.FS

func main() {
	r := gin.Default()

	sub, err := fs.Sub(files, "templates")
	if err != nil {
		panic(err)
	}
	tmpl := template.Must(template.ParseFS(sub, "*.tmpl"))
	r.SetHTMLTemplate(tmpl)
	r.GET("/", func(c *gin.Context) {
		c.HTML(200, "index.tmpl", gin.H{
			"title": "github.com/wujunwei928/parse-video Demo",
		})
	})

	r.GET("/video/share/url/parse", func(c *gin.Context) {
		paramUrl := c.Query("url")
		parseRes, err := parser.ParseVideoShareUrlByRegexp(paramUrl)
		jsonRes := HttpResponse{
			Code: 200,
			Msg:  "解析成功",
			Data: parseRes,
		}
		if err != nil {
			jsonRes = HttpResponse{
				Code: 201,
				Msg:  err.Error(),
			}
		}

		c.JSON(http.StatusOK, jsonRes)
	})

	r.GET("/video/id/parse", func(c *gin.Context) {
		videoId := c.Query("video_id")
		source := c.Query("source")

		parseRes, err := parser.ParseVideoId(source, videoId)
		jsonRes := HttpResponse{
			Code: 200,
			Msg:  "解析成功",
			Data: parseRes,
		}
		if err != nil {
			jsonRes = HttpResponse{
				Code: 201,
				Msg:  err.Error(),
			}
		}

		c.JSON(200, jsonRes)
	})

	// 证书文件路径
	certFile := "redjue.top_public.crt"
	keyFile := "redjue.top.key"

	// HTTP服务器配置
	httpSrv := &http.Server{
		Addr:    ":7777",
		Handler: r,
	}

	// HTTPS服务器配置
	httpsSrv := &http.Server{
		Addr:    ":7778", // 使用HTTPS端口
		Handler: r,
	}

	// 启动HTTP服务
	go func() {
		log.Println("HTTP Server starting on :7777...")
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP listen error: %s\n", err)
		}
	}()

	// 启动HTTPS服务，使用SSL证书
	go func() {
		log.Println("HTTPS Server starting on :7778...")
		if err := httpsSrv.ListenAndServeTLS(certFile, keyFile); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTPS listen error: %s\n", err)
		}
	}()

	// 等待中断信号以优雅地关闭服务器 (设置 5 秒的超时时间)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	<-quit
	log.Println("Shutdown Servers ...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 关闭两个服务器
	if err := httpSrv.Shutdown(ctx); err != nil {
		log.Fatal("HTTP Server Shutdown:", err)
	}

	if err := httpsSrv.Shutdown(ctx); err != nil {
		log.Fatal("HTTPS Server Shutdown:", err)
	}

	log.Println("Servers exiting")
}
