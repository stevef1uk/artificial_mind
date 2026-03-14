package display

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"
)

type Status struct {
	Status           string `json:"status,omitempty"`
	Emoji            string `json:"emoji,omitempty"`
	Text             string `json:"text,omitempty"`
	RGB              string `json:"RGB,omitempty"`
	Brightness       int    `json:"brightness,omitempty"`
	ScrollSpeed      int    `json:"scroll_speed,omitempty"`
	BatteryLevel     int    `json:"battery_level,omitempty"`
	BatteryColor     string `json:"battery_color,omitempty"`
	Image            string `json:"image,omitempty"`
	CaptureImagePath string `json:"capture_image_path,omitempty"`
	CameraMode       *bool  `json:"camera_mode,omitempty"`
}

type Event struct {
	Event string `json:"event"`
}

type Client struct {
	addr             string
	conn             net.Conn
	OnButtonPressed  func()
	OnButtonReleased func()
	OnCameraCapture  func()
	OnCameraExit     func()
	mu               sync.Mutex
	EventChan        chan string
	closed           bool
}

func NewClient(addr string) (*Client, error) {
	c := &Client{
		addr:      addr,
		EventChan: make(chan string, 10),
	}

	if err := c.connect(); err != nil {
		return nil, err
	}

	go c.listenLoop()
	return c, nil
}

func (c *Client) connect() error {
	var err error
	for i := 0; i < 5; i++ {
		c.conn, err = net.DialTimeout("tcp", c.addr, 5*time.Second)
		if err == nil {
			fmt.Printf("Connected to display at %s\n", c.addr)
			return nil
		}
		fmt.Printf("Display connection failed (%s), retrying in 2s... (%d/5)\n", err, i+1)
		time.Sleep(2 * time.Second)
	}
	return err
}

func (c *Client) listenLoop() {
	for {
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return
		}
		conn := c.conn
		c.mu.Unlock()

		if conn == nil {
			if err := c.connect(); err != nil {
				fmt.Printf("Permanent connection loss: %v\n", err)
				close(c.EventChan)
				return
			}
			continue
		}

		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "OK" {
				continue
			}

			var ev Event
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				continue
			}

			switch ev.Event {
			case "button_pressed":
				c.EventChan <- "button_pressed"
			case "button_released":
				c.EventChan <- "button_released"
			case "camera_capture":
				c.EventChan <- "camera_capture"
			case "exit_camera_mode":
				c.EventChan <- "exit_camera_mode"
			}
		}

		if err := scanner.Err(); err != nil {
			fmt.Printf("Display socket read error: %v\n", err)
		} else {
			fmt.Println("Display connection closed by server")
		}

		c.mu.Lock()
		c.conn = nil
		c.mu.Unlock()
		time.Sleep(1 * time.Second)
	}
}

func (c *Client) Send(s Status) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	data, err := json.Marshal(s)
	if err != nil {
		return err
	}

	_, err = c.conn.Write(append(data, '\n'))
	return err
}

func (c *Client) Display(s Status) error {
	return c.Send(s)
}

func (c *Client) Close() error {
	c.mu.Lock()
	c.closed = true
	if c.conn != nil {
		c.conn.Close()
	}
	c.mu.Unlock()
	return nil
}
