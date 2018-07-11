package engine

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

type Client struct {
	*Socket
	id           string
	pingInterval time.Duration
	pingTimeout  time.Duration
	closeChan    chan struct{}
	once         sync.Once
}

func Dial(rawurl string, requestHeader http.Header, dialer Dialer) (c *Client, err error) {
	conn, err := dialer.Dial(rawurl, requestHeader)
	if err != nil {
		return
	}
	p, err := conn.ReadPacket()
	if err != nil {
		return
	}
	if p.pktType != PacketTypeOpen {
		err = ErrInvalidPayload
		return
	}
	var param Parameters
	if err = json.Unmarshal(p.data, &param); err != nil {
		return
	}
	pingInterval := time.Duration(param.PingInterval) * time.Millisecond
	pingTimeout := time.Duration(param.PingTimeout) * time.Millisecond

	closeChan := make(chan struct{}, 1)
	so := &Socket{Conn: conn, eventHandlers: newEventHandlers(), readTimeout: pingTimeout, writeTimeout: pingTimeout}
	c = &Client{
		Socket:       so,
		pingInterval: pingInterval,
		pingTimeout:  pingTimeout,
		closeChan:    closeChan,
		id:           param.SID,
	}

	go func() {
		for {
			select {
			case <-closeChan:
				return
			case <-time.After(pingInterval):
			}
			if err = c.Ping(); err != nil {
				println(err.Error())
				return
			}
		}
	}()
	go func() {
		defer so.Close()
		for {
			select {
			case <-closeChan:
				return
			default:
			}
			if err := so.Handle(); err != nil {
				println(err.Error())
				return
			}
		}
	}()

	return
}

func (c *Client) Ping() error {
	return c.Emit(EventPing, nil)
}

func (c *Client) Close() (err error) {
	c.once.Do(func() {
		close(c.closeChan)
		err = c.Conn.Close()
	})
	return
}

func (c *Client) Id() string {
	return c.id
}