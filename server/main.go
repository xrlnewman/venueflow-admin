package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
)

type idempotencyStore interface {
	Get(context.Context, string) (string, bool, error)
	Set(context.Context, string, string) error
	Lock(context.Context, string) (func(), error)
}

type redisIdempotency struct{ client *redis.Client }

func (r *redisIdempotency) Get(ctx context.Context, key string) (string, bool, error) {
	v, err := r.client.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", false, nil
	}
	return v, err == nil, err
}
func (r *redisIdempotency) Set(ctx context.Context, key, value string) error {
	return r.client.Set(ctx, key, value, 24*time.Hour).Err()
}
func (r *redisIdempotency) Lock(ctx context.Context, key string) (func(), error) {
	token := fmt.Sprintf("%d", time.Now().UnixNano())
	ok, err := r.client.SetNX(ctx, "lock:"+key, token, 15*time.Second).Result()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("资源正在处理中")
	}
	return func() { _ = r.client.Del(context.Background(), "lock:"+key).Err() }, nil
}

type memoryRuntimeIdem struct {
	*memoryIdempotency
	lock sync.Mutex
}

func newMemoryRuntimeIdem() *memoryRuntimeIdem {
	return &memoryRuntimeIdem{memoryIdempotency: newMemoryIdempotency()}
}
func (m *memoryRuntimeIdem) Lock(_ context.Context, _ string) (func(), error) {
	m.lock.Lock()
	return m.lock.Unlock, nil
}

type app struct {
	store fleetStore
	idem  idempotencyStore
}

func traceID() string { return fmt.Sprintf("ff-%d", time.Now().UnixNano()) }
func respond(c *gin.Context, status int, data any, message string) {
	c.JSON(status, gin.H{"code": statusCode(status), "message": message, "data": data, "traceId": traceID()})
}
func statusCode(status int) int {
	if status == http.StatusOK || status == http.StatusCreated {
		return 0
	}
	return status
}
func ok(c *gin.Context, data any) { respond(c, http.StatusOK, data, "ok") }
func fail(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, errNotFound) {
		status = http.StatusNotFound
	}
	if errors.Is(err, errInvalidTransition) || errors.Is(err, errDuplicate) || errors.Is(err, errInventoryExceeded) || strings.Contains(err.Error(), "处理中") {
		status = http.StatusConflict
	}
	if strings.Contains(err.Error(), "不能为空") || strings.Contains(err.Error(), "请求") {
		status = http.StatusBadRequest
	}
	respond(c, status, gin.H{}, err.Error())
}

func (a *app) write(c *gin.Context, key, resource string, fn func() (any, error)) {
	a.writeStatus(c, key, resource, http.StatusOK, fn)
}

func (a *app) writeStatus(c *gin.Context, key, resource string, successStatus int, fn func() (any, error)) {
	if strings.TrimSpace(key) == "" {
		fail(c, errors.New("请求必须携带 Idempotency-Key"))
		return
	}
	ctx := c.Request.Context()
	cacheKey := "venueflow:" + resource + ":" + key
	if raw, found, err := a.idem.Get(ctx, cacheKey); err != nil {
		fail(c, err)
		return
	} else if found {
		var data any
		if json.Unmarshal([]byte(raw), &data) != nil {
			fail(c, errors.New("幂等结果损坏"))
			return
		}
		c.Header("X-Idempotent-Replay", "true")
		respond(c, successStatus, data, "ok")
		return
	}
	release, err := a.idem.Lock(ctx, cacheKey)
	if err != nil {
		fail(c, err)
		return
	}
	defer release()
	if raw, found, _ := a.idem.Get(ctx, cacheKey); found {
		var data any
		_ = json.Unmarshal([]byte(raw), &data)
		c.Header("X-Idempotent-Replay", "true")
		respond(c, successStatus, data, "ok")
		return
	}
	data, err := fn()
	if err != nil {
		fail(c, err)
		return
	}
	raw, _ := json.Marshal(data)
	if err = a.idem.Set(ctx, cacheKey, string(raw)); err != nil {
		fail(c, err)
		return
	}
	respond(c, successStatus, data, "ok")
}

func (a *app) routes() *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	r.GET("/healthz", func(c *gin.Context) {
		ok(c, gin.H{"service": "venueflow", "time": time.Now().UTC(), "storage": fmt.Sprintf("%T", a.store)})
	})
	api := r.Group("/api/v1")
	api.GET("/venues", func(c *gin.Context) {
		list := a.store.listVenues(c)
		ok(c, gin.H{"list": list, "total": len(list)})
	})
	api.GET("/sessions", func(c *gin.Context) {
		page, size := pageParams(c)
		list, total := a.store.listSessions(c, c.Query("status"), page, size)
		ok(c, gin.H{"list": list, "total": total, "page": page, "pageSize": size})
	})
	api.GET("/sessions/:id", func(c *gin.Context) {
		item, err := a.store.getSession(c, c.Param("id"))
		if err != nil {
			fail(c, err)
			return
		}
		ok(c, item)
	})
	api.GET("/sessions/:id/events", func(c *gin.Context) {
		events, err := a.store.listSessionEvents(c, c.Param("id"))
		if err != nil {
			fail(c, err)
			return
		}
		ok(c, events)
	})
	api.POST("/sessions", func(c *gin.Context) {
		var body struct {
			VenueID, Title, StartsAt, EndsAt string
			Capacity                         int
			Price                            float64
		}
		if c.ShouldBindJSON(&body) != nil {
			fail(c, errors.New("请求体格式不正确"))
			return
		}
		starts, err1 := time.Parse(time.RFC3339, body.StartsAt)
		ends, err2 := time.Parse(time.RFC3339, body.EndsAt)
		if err1 != nil || err2 != nil {
			fail(c, errInvalidInput)
			return
		}
		in := Session{ID: fmt.Sprintf("VS-%s", time.Now().UTC().Format("060102150405.000")), VenueID: body.VenueID, Title: body.Title, StartsAt: starts, EndsAt: ends, Capacity: body.Capacity, Price: body.Price}
		a.writeStatus(c, c.GetHeader("Idempotency-Key"), "session:create", http.StatusCreated, func() (any, error) { return a.store.createSession(c, in) })
	})
	api.POST("/sessions/:id/publish", func(c *gin.Context) {
		var body struct {
			Actor string `json:"actor"`
		}
		if c.ShouldBindJSON(&body) != nil {
			body.Actor = "运营人员"
		}
		a.write(c, c.GetHeader("Idempotency-Key"), "session:publish:"+c.Param("id"), func() (any, error) { return a.store.publishSession(c, c.Param("id"), body.Actor) })
	})
	api.POST("/sessions/:id/sell", func(c *gin.Context) {
		var body struct {
			Quantity int    `json:"quantity"`
			Actor    string `json:"actor"`
		}
		if c.ShouldBindJSON(&body) != nil || body.Quantity <= 0 {
			fail(c, errInvalidInput)
			return
		}
		a.write(c, c.GetHeader("Idempotency-Key"), "session:sell:"+c.Param("id"), func() (any, error) {
			session, tickets, err := a.store.sellSession(c, c.Param("id"), body.Quantity, body.Actor)
			return gin.H{"session": session, "tickets": tickets}, err
		})
	})
	api.POST("/sessions/:id/checkin", func(c *gin.Context) {
		var body struct {
			TicketCode string `json:"ticketCode"`
			Actor      string `json:"actor"`
		}
		if c.ShouldBindJSON(&body) != nil || strings.TrimSpace(body.TicketCode) == "" {
			fail(c, errInvalidInput)
			return
		}
		a.write(c, c.GetHeader("Idempotency-Key"), "session:checkin:"+c.Param("id")+":"+body.TicketCode, func() (any, error) { return a.store.checkinSession(c, c.Param("id"), body.TicketCode, body.Actor) })
	})
	api.POST("/sessions/:id/status", func(c *gin.Context) {
		var body struct{ Status, Actor string }
		if c.ShouldBindJSON(&body) != nil || strings.TrimSpace(body.Status) == "" {
			fail(c, errInvalidInput)
			return
		}
		a.write(c, c.GetHeader("Idempotency-Key"), "session:status:"+c.Param("id")+":"+body.Status, func() (any, error) { return a.store.transitionSession(c, c.Param("id"), body.Status, body.Actor) })
	})
	api.POST("/sessions/:id/settle", func(c *gin.Context) {
		var body struct {
			Actor string `json:"actor"`
		}
		_ = c.ShouldBindJSON(&body)
		a.write(c, c.GetHeader("Idempotency-Key"), "session:settle:"+c.Param("id"), func() (any, error) { return a.store.settleSession(c, c.Param("id"), body.Actor) })
	})
	api.GET("/dashboard", func(c *gin.Context) {
		list, total := a.store.listShipments(c, "", 1, 1000)
		exceptions := a.store.listExceptions(c, "待处理")
		inTransit := 0
		completed := 0
		for _, item := range list {
			if item.Status == ShipmentInTransit {
				inTransit++
			}
			if item.Status == ShipmentCompleted {
				completed++
			}
		}
		rate := 0.0
		if total > 0 {
			rate = float64(completed) / float64(total) * 100
		}
		ok(c, gin.H{"todayShipments": total, "onTimeRate": rate, "inTransit": inTransit, "openExceptions": len(exceptions)})
	})
	api.GET("/shipments", func(c *gin.Context) {
		page, size := pageParams(c)
		list, total := a.store.listShipments(c, c.Query("status"), page, size)
		ok(c, gin.H{"list": list, "total": total, "page": page, "pageSize": size})
	})
	api.GET("/shipments/:id", func(c *gin.Context) {
		item, err := a.store.getShipment(c, c.Param("id"))
		if err != nil {
			fail(c, err)
			return
		}
		ok(c, item)
	})
	api.GET("/shipments/:id/events", func(c *gin.Context) {
		events, err := a.store.listShipmentEvents(c, c.Param("id"))
		if err != nil {
			fail(c, err)
			return
		}
		ok(c, events)
	})
	api.POST("/shipments", func(c *gin.Context) {
		var in Shipment
		if c.ShouldBindJSON(&in) != nil {
			fail(c, errors.New("请求体格式不正确"))
			return
		}
		if in.ID == "" {
			in.ID = fmt.Sprintf("FF-%s", time.Now().UTC().Format("060102150405.000"))
		}
		a.write(c, c.GetHeader("Idempotency-Key"), "shipment:create", func() (any, error) { return a.store.createShipment(c, in) })
	})
	api.POST("/shipments/:id/assign", func(c *gin.Context) {
		var body struct {
			Driver string `json:"driver"`
			Actor  string `json:"actor"`
		}
		if c.ShouldBindJSON(&body) != nil || strings.TrimSpace(body.Driver) == "" {
			fail(c, errors.New("场馆工作人员不能为空"))
			return
		}
		if body.Actor == "" {
			body.Actor = "调度主管"
		}
		a.write(c, c.GetHeader("Idempotency-Key"), "shipment:assign:"+c.Param("id"), func() (any, error) { return a.store.assignShipment(c, c.Param("id"), body.Driver, body.Actor) })
	})
	api.POST("/shipments/:id/status", func(c *gin.Context) {
		var body struct{ Status, Actor, Note string }
		if c.ShouldBindJSON(&body) != nil || strings.TrimSpace(body.Status) == "" {
			fail(c, errors.New("请求必须包含 status"))
			return
		}
		if body.Actor == "" {
			body.Actor = "运营人员"
		}
		a.write(c, c.GetHeader("Idempotency-Key"), "shipment:status:"+c.Param("id")+":"+body.Status, func() (any, error) {
			return a.store.transitionShipment(c, c.Param("id"), body.Status, body.Actor, body.Note)
		})
	})
	api.GET("/drivers", func(c *gin.Context) { list := a.store.listDrivers(c); ok(c, gin.H{"list": list, "total": len(list)}) })
	api.GET("/vehicles", func(c *gin.Context) { list := a.store.listVehicles(c); ok(c, gin.H{"list": list, "total": len(list)}) })
	api.GET("/exceptions", func(c *gin.Context) {
		list := a.store.listExceptions(c, c.Query("status"))
		ok(c, gin.H{"list": list, "total": len(list)})
	})
	api.POST("/exceptions/:id/resolve", func(c *gin.Context) {
		a.write(c, c.GetHeader("Idempotency-Key"), "exception:resolve:"+c.Param("id"), func() (any, error) { return a.store.resolveException(c, c.Param("id")) })
	})
	api.GET("/settlements", func(c *gin.Context) {
		list := a.store.listSettlements(c)
		ok(c, gin.H{"list": list, "amount": sumSettlement(list), "pending": pendingSettlement(list)})
	})
	api.POST("/settlements/:id/confirm", func(c *gin.Context) {
		a.write(c, c.GetHeader("Idempotency-Key"), "settlement:confirm:"+c.Param("id"), func() (any, error) { return a.store.confirmSettlement(c, c.Param("id")) })
	})
	return r
}

func pageParams(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	return page, size
}
func sumSettlement(list []Settlement) float64 {
	var n float64
	for _, v := range list {
		n += v.Amount
	}
	return n
}
func pendingSettlement(list []Settlement) float64 {
	var n float64
	for _, v := range list {
		if v.Status != "已结算" {
			n += v.Amount
		}
	}
	return n
}

func openMySQL(dsn string) (*sql.DB, error) {
	var db *sql.DB
	var err error
	for i := 0; i < 60; i++ {
		db, err = sql.Open("mysql", dsn)
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			err = db.PingContext(ctx)
			cancel()
			if err == nil {
				return db, nil
			}
			db.Close()
		}
		time.Sleep(2 * time.Second)
	}
	return nil, err
}
func main() {
	var store fleetStore = newMemoryStore()
	var idem idempotencyStore = newMemoryRuntimeIdem()
	if dsn := os.Getenv("MYSQL_DSN"); dsn != "" {
		db, err := openMySQL(dsn)
		if err != nil {
			log.Fatal(err)
		}
		store = &sqlStore{db: db}
	}
	if addr := os.Getenv("REDIS_ADDR"); addr != "" {
		client := redis.NewClient(&redis.Options{Addr: addr, Password: os.Getenv("REDIS_PASSWORD")})
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		if err := client.Ping(ctx).Err(); err != nil {
			log.Fatal(err)
		}
		cancel()
		idem = &redisIdempotency{client: client}
	}
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	log.Printf("venueflow listening on %s", addr)
	if err := http.ListenAndServe(addr, (&app{store: store, idem: idem}).routes()); err != nil {
		log.Fatal(err)
	}
}
