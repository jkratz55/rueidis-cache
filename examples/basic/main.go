package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/redis/rueidis"

	cache "github.com/jkratz55/rueidis-cache"
)

type Person struct {
	FirstName string
	LastName  string
	Age       int
}

func main() {

	// Initialize a logger. You can use any logger you wish but in this example
	// we are sticking to the standard library using slog.
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		AddSource: true,
	}))

	// Initialize rueidis redis client
	client, err := rueidis.NewClient(rueidis.ClientOption{
		InitAddress: []string{"localhost:6379"},
	})
	if err != nil {
		logger.Error("error creating redis client", slog.String("err", err.Error()))
		panic(err)
	}
	defer client.Close()

	// Ping Redis to ensure its reachable
	func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		err := client.Do(ctx, client.B().Ping().Build()).Error()
		if err != nil {
			logger.Error("error pinging redis", slog.String("err", err.Error()))
			panic(err)
		}
	}()

	// Initialize the Cache. This will use the default configuration but optionally
	// the serialization and compression can be swapped out and features like NearCaching
	// can be enabled.
	rdb := cache.New(client, cache.JSON()) // Configures the cache to use JSON for serialization

	if err := rdb.Set(context.Background(), "person", Person{
		FirstName: "Biily",
		LastName:  "Bob",
		Age:       45,
	}, 0); err != nil {
		panic("ohhhhh snap!")
	}

	var p Person
	if err := rdb.Get(context.Background(), "person", &p); err != nil {
		panic("ohhhhh snap")
	}
	fmt.Printf("%v\n", p)

	if err := rdb.Delete(context.Background(), "person"); err != nil {
		panic("ohhh snap!")
	}

	if err := rdb.Get(context.Background(), "person", &p); !errors.Is(err, cache.ErrKeyNotFound) {
		panic("ohhhhh snap, this key should be gone!")
	}
}
