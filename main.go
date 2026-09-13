package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/mmcdole/gofeed"
	"github.com/pelletier/go-toml/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB
var FP *gofeed.Parser

func DBHelper[T any]() gorm.Interface[T] {
	return gorm.G[T](DB)
}

func initDB() {
	var err error
	DB, err = gorm.Open(sqlite.Open("./feed.db"), &gorm.Config{
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

type FeedItem struct {
	FeedTitle string
	Title     string
	GUID      string `gorm:"primaryKey"`
}

func queryFeed(feedUrl string) (feed *gofeed.Feed, err error) {
	return FP.ParseURL(feedUrl)
}

func notifyAboutItem(feed *gofeed.Feed, item *gofeed.Item) {}

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
	feed, _ := queryFeed(feedUrl)
	for _, item := range feed.Items {
		processItem(feed, item)
	}
}

func processFeeds() {
	for _, feed := range CONFIG.Feeds {
		processFeed(feed)
	}
}

type Config struct {
	Feeds                 []string
	RefreshTimerInSeconds int
}

var CONFIG *Config

func runOnTick(ticker *time.Ticker, done chan bool, f func()) {
	for {
		select {
		case <-done:
			return
		case t := <-ticker.C:
			fmt.Println("Tick at", t)
			f()
		}
	}
}

func main() {
	initDB()
	initFeedParser()

	doc := `
	feeds = [
	"https://novumvita.github.io/index.xml",
	"https://naveensd.com/rss.xml",
	]
	refreshTimerInSeconds = 5
	`

	err := toml.Unmarshal([]byte(doc), &CONFIG)
	if err != nil {
		panic(err)
	}

	ticker := time.NewTicker(time.Duration(CONFIG.RefreshTimerInSeconds) * time.Second)
	defer ticker.Stop()
	done := make(chan bool)

	go runOnTick(ticker, done, processFeeds)

	time.Sleep(30 * time.Second)
	done <- true
	fmt.Println("Ticker stopped")

}
