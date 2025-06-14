package main

import (
	"flag"
	"fmt"
	"log"
	"market-api/internal/config"
	"net/http"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/rest/httpx"
)

var configFile = flag.String("f", "etc/conf.yaml", "the config file")

func main() {
	// flag.Parse()
	// //日志的打印格式替换一下
	// logx.MustSetup(logx.LogConf{Stat: false, Encoding: "plain"})
	// var c config.Config
	// conf.MustLoad(*configFile, &c)
	// wsServer := ws.NewWebsocketServer("/socket.io")
	// server := rest.MustNewServer(
	// 	c.RestConf,
	// 	rest.WithChain(chain.New(wsServer.ServerHandler)),
	// 	//rest.WithRouter(自定义的路由实现) zero框架就会走你的路由
	// 	rest.WithCustomCors(func(header http.Header) {
	// 		header.Set("Access-Control-Allow-Headers", "DNT,X-Mx-ReqToken,Keep-Alive,User-Agent,X-Requested-With,If-Modified-Since,Cache-Control,Content-Type,Authorization,token,x-auth-token")
	// 	}, nil, "*"))
	// defer server.Stop()

	// ctx := svc.NewServiceContext(c, wsServer)
	// router := handler.NewRouters(server, c.Prefix)
	// handler.RegisterHandlers(router, ctx)

	var restConf rest.RestConf
	var c config.Config
	conf.MustLoad(*configFile, &c)
    s, err := rest.NewServer(restConf)
    if err != nil {
        log.Fatal(err)
        return
    }

    s.AddRoute(rest.Route{ // 添加路由
        Method: http.MethodGet,
        Path:   "/hello/world",
        Handler: func(writer http.ResponseWriter, request *http.Request) { // 处理函数
            httpx.OkJson(writer, "Hello World!")
        },
    })
    defer s.Stop()
	
	fmt.Printf("Starting server ...\n")
    s.Start() // 启动服务
}
