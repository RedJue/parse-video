package main

import (
	"context"
	"crypto/tls"
	"embed"
	"fmt"
	"html/template"
	"io"
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

	// 新增: 直接返回视频流的接口
	r.GET("/video/stream", func(c *gin.Context) {
		videoUrl := c.Query("url")
		if videoUrl == "" {
			c.JSON(http.StatusBadRequest, HttpResponse{
				Code: 400,
				Msg:  "视频URL不能为空",
			})
			return
		}

		// 检测是否来自微信环境
		userAgent := c.Request.UserAgent()
		isWechat := false
		if len(userAgent) > 0 {
			// 微信环境检测 - 包含MicroMessenger或miniProgram字样
			if containsWechat := (func() bool {
				ua := userAgent
				return (len(ua) > 0 && (len(ua) > 15 && ua[len(ua)-15:] == "miniProgram")) ||
					(len(ua) > 0 && ua[:15] == "MicroMessenger")
			})(); containsWechat {
				isWechat = true
				log.Printf("检测到微信环境请求: %s", userAgent)
			}
		}

		// 创建HTTP客户端, 微信环境可能需要更短的超时时间
		client := &http.Client{
			Timeout: 20 * time.Second, // 缩短超时时间
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // 跳过证书验证
				// 增加传输层超时设置
				ResponseHeaderTimeout: 10 * time.Second,
				ExpectContinueTimeout: 10 * time.Second,
				// 允许重用连接
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     30 * time.Second,
			},
		}

		// 发送请求获取视频
		req, err := http.NewRequest("GET", videoUrl, nil)
		if err != nil {
			c.JSON(http.StatusInternalServerError, HttpResponse{
				Code: 500,
				Msg:  "创建请求失败: " + err.Error(),
			})
			return
		}

		// 微信环境下，只传递必要的请求头，避免不兼容
		if isWechat {
			// 只设置基本请求头，避免不兼容
			req.Header.Set("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 13_2_3 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/13.0.3 Mobile/15E148 Safari/604.1")
			// 保留Range请求头以支持分片下载
			if rangeHeader := c.Request.Header.Get("Range"); rangeHeader != "" {
				req.Header.Set("Range", rangeHeader)
			}
			req.Header.Set("Accept", "*/*")
			req.Header.Set("Accept-Encoding", "gzip, deflate")
			req.Header.Set("Connection", "keep-alive")
		} else {
			// 在非微信环境下，转发所有请求头
			for name, values := range c.Request.Header {
				// 跳过一些可能导致问题的头
				if name != "Connection" && name != "Sec-Fetch-Mode" {
					for _, value := range values {
						req.Header.Add(name, value)
					}
				}
			}
		}

		// 确保有User-Agent头
		if req.Header.Get("User-Agent") == "" {
			req.Header.Set("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 13_2_3 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/13.0.3 Mobile/15E148 Safari/604.1")
		}

		log.Printf("请求视频URL: %s, 是否微信环境: %v", videoUrl, isWechat)
		resp, err := client.Do(req)
		if err != nil {
			errMsg := fmt.Sprintf("获取视频失败: %s", err.Error())
			log.Println(errMsg)
			c.JSON(http.StatusInternalServerError, HttpResponse{
				Code: 500,
				Msg:  errMsg,
			})
			return
		}
		defer resp.Body.Close()

		// 检查响应状态
		if resp.StatusCode >= 400 {
			errMsg := fmt.Sprintf("视频源服务器返回状态码: %d", resp.StatusCode)
			log.Println(errMsg)
			c.JSON(http.StatusBadGateway, HttpResponse{
				Code: 502,
				Msg:  errMsg,
			})
			return
		}

		// 清除之前可能设置的所有响应头
		for k := range c.Writer.Header() {
			c.Writer.Header().Del(k)
		}

		// 微信环境下，只设置必要的响应头
		if isWechat {
			// 根据内容类型设置
			contentType := resp.Header.Get("Content-Type")
			if contentType == "" {
				contentType = "video/mp4" // 默认类型
			}
			c.Writer.Header().Set("Content-Type", contentType)

			// 处理Range响应
			if resp.Header.Get("Content-Range") != "" {
				c.Writer.Header().Set("Content-Range", resp.Header.Get("Content-Range"))
			}
			if resp.Header.Get("Content-Length") != "" {
				c.Writer.Header().Set("Content-Length", resp.Header.Get("Content-Length"))
			}

			// 必要的跨域设置
			c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
			c.Writer.Header().Set("Access-Control-Allow-Methods", "GET")
			c.Writer.Header().Set("Accept-Ranges", "bytes")
		} else {
			// 非微信环境，转发所有响应头
			for name, values := range resp.Header {
				// 跳过一些可能导致问题的响应头
				if name != "Connection" && name != "Transfer-Encoding" {
					for _, value := range values {
						c.Writer.Header().Add(name, value)
					}
				}
			}

			// 设置跨域头
			c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
			c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			c.Writer.Header().Set("Access-Control-Allow-Headers", "*")
			c.Writer.Header().Set("Access-Control-Expose-Headers", "*")
		}

		// 设置状态码
		c.Status(resp.StatusCode)

		// 直接转发响应体
		written, err := io.Copy(c.Writer, resp.Body)
		if err != nil {
			log.Printf("传输视频流时出错: %v, 已传输字节: %d", err, written)
		} else {
			log.Printf("成功传输视频流，总字节: %d", written)
		}
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
