package server

import (
	"fmt"
	"time"
	"ui-backend-for-omotebako-site-controller/app/server/handlers"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type Server struct {
	gin  *gin.Engine
	ws   *websocket.Upgrader
	port string
}

func NewServer(port interface{}, handler *handlers.SCHandler) *Server {
	return &Server{
		gin: gin.New(),
		ws: &websocket.Upgrader{
			HandshakeTimeout:  5 * time.Second,
			ReadBufferSize:    0,
			WriteBufferSize:   0,
			WriteBufferPool:   nil,
			Subprotocols:      nil,
			Error:             nil,
			CheckOrigin:       nil,
			EnableCompression: false,
		},
		port: fmt.Sprintf(`:%v`, port),
	}
}
