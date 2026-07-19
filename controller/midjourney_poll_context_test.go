package controller

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMidjourneyPollCancellationClosesBlackholeBeforeNextLeaseOwner(t *testing.T) {
	service.InitHttpClient()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	firstStarted := make(chan struct{})
	firstClosed := make(chan error, 1)
	serverDone := make(chan error, 1)
	go func() {
		first, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverDone <- acceptErr
			return
		}
		_ = first.SetDeadline(time.Now().Add(3 * time.Second))
		firstReader := bufio.NewReader(first)
		request, readErr := http.ReadRequest(firstReader)
		if readErr != nil {
			_ = first.Close()
			serverDone <- readErr
			return
		}
		_, readErr = io.Copy(io.Discard, request.Body)
		_ = request.Body.Close()
		if readErr != nil {
			_ = first.Close()
			serverDone <- readErr
			return
		}
		close(firstStarted)

		oneByte := make([]byte, 1)
		_, closeErr := firstReader.Read(oneByte)
		_ = first.Close()
		firstClosed <- closeErr

		second, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverDone <- acceptErr
			return
		}
		defer second.Close()
		_ = second.SetDeadline(time.Now().Add(3 * time.Second))
		secondRequest, readErr := http.ReadRequest(bufio.NewReader(second))
		if readErr != nil {
			serverDone <- readErr
			return
		}
		_, readErr = io.Copy(io.Discard, secondRequest.Body)
		_ = secondRequest.Body.Close()
		if readErr != nil {
			serverDone <- readErr
			return
		}
		_, writeErr := io.WriteString(second, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 2\r\nConnection: close\r\n\r\n[]")
		serverDone <- writeErr
	}()

	originalDB := model.DB
	database, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:midjourney-poll-context-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := database.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, database.AutoMigrate(&model.Channel{}, &model.Midjourney{}))
	model.DB = database
	originalMemoryCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		model.DB = originalDB
		common.MemoryCacheEnabled = originalMemoryCache
	})

	baseURL := "http://" + listener.Addr().String()
	channel := &model.Channel{Id: 991, Name: "midjourney-poll", Key: "secret", BaseURL: &baseURL, Status: common.ChannelStatusEnabled}
	require.NoError(t, database.Create(channel).Error)
	task := &model.Midjourney{MjId: "upstream-mj", ChannelId: channel.Id, Status: "IN_PROGRESS", Progress: "20%"}
	require.NoError(t, database.Create(task).Error)

	ctx, cancel := context.WithCancel(context.Background())
	firstRun := make(chan midjourneyPollSummary, 1)
	go func() { firstRun <- runMidjourneyTaskUpdateOnce(ctx, nil) }()
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("Midjourney poll did not reach the provider blackhole")
	}
	cancel()
	select {
	case <-firstRun:
	case <-time.After(time.Second):
		t.Fatal("canceled Midjourney poll outlived its lease")
	}
	select {
	case closeErr := <-firstClosed:
		if netErr, ok := closeErr.(net.Error); ok && netErr.Timeout() {
			t.Fatalf("provider connection outlived the canceled lease: %v", closeErr)
		}
		require.Error(t, closeErr)
	case <-time.After(time.Second):
		t.Fatal("provider connection outlived the canceled lease")
	}

	secondSummary := runMidjourneyTaskUpdateOnce(context.Background(), nil)
	require.Equal(t, 1, secondSummary.ChannelsScanned)
	select {
	case serverErr := <-serverDone:
		require.NoError(t, serverErr)
	case <-time.After(time.Second):
		t.Fatal("replacement Midjourney lease owner did not complete")
	}
}
