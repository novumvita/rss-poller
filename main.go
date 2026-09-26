package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"slices"
	"time"

	"github.com/AnthonyHewins/gotfy"
	"github.com/mmcdole/gofeed"
	"github.com/pelletier/go-toml/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB
var FP *gofeed.Parser
var NTFY *gotfy.Publisher

func DBHelper[T any]() gorm.Interface[T] {
	return gorm.G[T](DB)
}

func initDB() {
	var err error
	DB, err = gorm.Open(sqlite.Open(CONFIG.DbPath), &gorm.Config{
		Logger: logger.New(
			log.Default(),
			logger.Config{
				IgnoreRecordNotFoundError: true,
			},
		),
		TranslateError: true,
	})
	if err != nil {
		log.Fatal(err)
	}
	DB.AutoMigrate(&FeedItem{})
}

func initFeedParser() {
	FP = gofeed.NewParser()
}

func initNtfyPublisher() {
	server, _ := url.Parse(CONFIG.NtfyServer)
	NTFY, _ = gotfy.NewPublisher(server)
}

type FeedItem struct {
	FeedTitle string
	Title     string
	GUID      string `gorm:"primaryKey"`
}

func queryFeed(feedUrl string) (feed *gofeed.Feed, err error) {
	return FP.ParseURL(feedUrl)
}

func notifyAboutItem(feed *gofeed.Feed, item *gofeed.Item) {
	itemUrl, err := url.Parse(item.GUID)
	if err != nil {
		log.Println("Error parsing item URL. Skipping notification.")
		return
	}
	_, err = NTFY.SendMessage(
		context.Background(), &gotfy.Message{
			Topic:    CONFIG.NtfyTopic,
			Title:    feed.Title,
			Message:  item.Title,
			ClickURL: itemUrl,
		},
	)
	if err != nil {
		log.Println("Error publishing notification.")
	} else {
		time.Sleep(time.Second) // Sleep to ensure notifications are properly ordered.
	}
}

func processItem(feed *gofeed.Feed, item *gofeed.Item) {
	c := context.Background()
	err := DBHelper[FeedItem]().Create(c, &FeedItem{FeedTitle: feed.Title, Title: item.Title, GUID: item.GUID})
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		log.Printf("Item already exists in DB: \"%s\". Skipping.\n", item.GUID)
	} else {
		log.Printf("New item: \"%s\"\n", item.GUID)
		notifyAboutItem(feed, item)
	}
}

func processFeed(feedUrl string) {
	feed, err := queryFeed(feedUrl)
	if err != nil {
		log.Println("Error processing feed.", err)
		return
	}
	// Backward should give us items from oldest to newest.
	// Unsure if this is part of spec. Might have to sort by item.Published to guarantee order.
	for _, item := range slices.Backward(feed.Items) {
		processItem(feed, item)
	}
}

func processFeeds() {
	if CONFIG.Feeds == nil {
		log.Println("No feeds to poll.")
		return
	}
	for _, feed := range CONFIG.Feeds {
		processFeed(feed)
	}
}

type Config struct {
	Feeds                 []string
	RefreshTimerInSeconds int
	NtfyTopic             string
	NtfyServer            string
	DbPath                string
}

var CONFIG *Config

func readConfig() {
	configPath := flag.String("config", "./config.toml", "Path to the config file.")
	flag.Parse()
	configData, err := os.ReadFile(*configPath)

	err = toml.Unmarshal(configData, &CONFIG)
	if err != nil {
		panic(err)
	}
}

func runOnTick(ticker *time.Ticker, manualTick chan bool, done chan bool, f func()) {
	for {
		select {
		case <-done:
			return
		case t := <-ticker.C:
			fmt.Println("Tick at", t)
			f()
		case <-manualTick:
			fmt.Println("Manual tick running.")
			f()
		}
	}
}

var MANUAL_TICK chan bool = make(chan bool)
var DONE chan bool = make(chan bool)

func tick(w http.ResponseWriter, req *http.Request) {
	MANUAL_TICK <- true
	fmt.Fprintf(w, "Manual tick triggered.")
}

func main() {
	readConfig()
	initDB()
	initFeedParser()
	initNtfyPublisher()

	processFeeds() // Call once so we don't have to wait for refresh time

	ticker := time.NewTicker(time.Duration(CONFIG.RefreshTimerInSeconds) * time.Second)

	go runOnTick(ticker, MANUAL_TICK, DONE, processFeeds)

	// TODO: HTTP server endpoints here which can:
	// 1. Add feeds - update config
	// 2. Stop via done

	http.HandleFunc("/tick", tick)
	http.ListenAndServe(":8090", nil)

	time.Sleep(30 * time.Second)
	DONE <- true
	fmt.Println("Ticker stopped")
}
