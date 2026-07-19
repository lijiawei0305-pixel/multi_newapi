package openai

import (
	"fmt"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func OpenaiRealtimeHandler(c *gin.Context, info *relaycommon.RelayInfo) (*types.NewAPIError, *dto.RealtimeUsage) {
	if info == nil || info.ClientWs == nil || info.TargetWs == nil {
		return types.NewError(fmt.Errorf("invalid websocket connection"), types.ErrorCodeBadResponse), nil
	}

	info.IsStream = true
	clientConn := info.ClientWs
	targetConn := info.TargetWs

	readerDone := make(chan struct{}, 2)
	errChan := make(chan error, 2)
	var readers sync.WaitGroup
	var stateMu sync.Mutex

	usage := &dto.RealtimeUsage{}
	localUsage := &dto.RealtimeUsage{}
	sumUsage := &dto.RealtimeUsage{}
	settlementFailed := false

	reportError := func(err error) {
		select {
		case errChan <- err:
		default:
		}
	}
	reportDone := func() {
		select {
		case readerDone <- struct{}{}:
		default:
		}
	}

	readers.Add(1)
	gopool.Go(func() {
		defer readers.Done()
		defer func() {
			if r := recover(); r != nil {
				reportError(fmt.Errorf("panic in client reader: %v", r))
			}
		}()
		for {
			select {
			case <-c.Done():
				return
			default:
				_, message, err := clientConn.ReadMessage()
				if err != nil {
					if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
						reportError(fmt.Errorf("error reading from client: %v", err))
					} else {
						reportDone()
					}
					return
				}

				realtimeEvent := &dto.RealtimeEvent{}
				err = common.Unmarshal(message, realtimeEvent)
				if err != nil {
					reportError(fmt.Errorf("error unmarshalling message: %v", err))
					return
				}

				stateMu.Lock()
				if realtimeEvent.Type == dto.RealtimeEventTypeSessionUpdate {
					if realtimeEvent.Session != nil {
						if realtimeEvent.Session.Tools != nil {
							info.RealtimeTools = realtimeEvent.Session.Tools
						}
					}
				}

				textToken, audioToken, err := service.CountTokenRealtime(info, *realtimeEvent, info.UpstreamModelName)
				if err != nil {
					stateMu.Unlock()
					reportError(fmt.Errorf("error counting text token: %v", err))
					return
				}
				logger.LogInfo(c, fmt.Sprintf("type: %s, textToken: %d, audioToken: %d", realtimeEvent.Type, textToken, audioToken))
				localUsage.TotalTokens += textToken + audioToken
				localUsage.InputTokens += textToken + audioToken
				localUsage.InputTokenDetails.TextTokens += textToken
				localUsage.InputTokenDetails.AudioTokens += audioToken
				stateMu.Unlock()

				err = helper.WssString(c, targetConn, string(message))
				if err != nil {
					reportError(fmt.Errorf("error writing to target: %v", err))
					return
				}
			}
		}
	})

	readers.Add(1)
	gopool.Go(func() {
		defer readers.Done()
		defer func() {
			if r := recover(); r != nil {
				reportError(fmt.Errorf("panic in target reader: %v", r))
			}
		}()
		for {
			select {
			case <-c.Done():
				return
			default:
				_, message, err := targetConn.ReadMessage()
				if err != nil {
					if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
						reportError(fmt.Errorf("error reading from target: %v", err))
					} else {
						reportDone()
					}
					return
				}
				service.MarkUpstreamAccepted(c)
				stateMu.Lock()
				info.SetFirstResponseTime()
				realtimeEvent := &dto.RealtimeEvent{}
				err = common.Unmarshal(message, realtimeEvent)
				if err != nil {
					stateMu.Unlock()
					reportError(fmt.Errorf("error unmarshalling message: %v", err))
					return
				}

				if realtimeEvent.Type == dto.RealtimeEventTypeResponseDone {
					if realtimeEvent.Response == nil {
						stateMu.Unlock()
						reportError(fmt.Errorf("response.done event is missing response payload"))
						return
					}
					realtimeUsage := realtimeEvent.Response.Usage
					if realtimeUsage != nil {
						usage.TotalTokens += realtimeUsage.TotalTokens
						usage.InputTokens += realtimeUsage.InputTokens
						usage.OutputTokens += realtimeUsage.OutputTokens
						usage.InputTokenDetails.AudioTokens += realtimeUsage.InputTokenDetails.AudioTokens
						usage.InputTokenDetails.CachedTokens += realtimeUsage.InputTokenDetails.CachedTokens
						usage.InputTokenDetails.TextTokens += realtimeUsage.InputTokenDetails.TextTokens
						usage.OutputTokenDetails.AudioTokens += realtimeUsage.OutputTokenDetails.AudioTokens
						usage.OutputTokenDetails.TextTokens += realtimeUsage.OutputTokenDetails.TextTokens
						err := preConsumeUsage(c, info, usage, sumUsage)
						if err != nil {
							settlementFailed = true
							stateMu.Unlock()
							reportError(fmt.Errorf("error consume usage: %v", err))
							return
						}
						// 本次计费完成，清除
						usage = &dto.RealtimeUsage{}

						localUsage = &dto.RealtimeUsage{}
					} else {
						textToken, audioToken, err := service.CountTokenRealtime(info, *realtimeEvent, info.UpstreamModelName)
						if err != nil {
							stateMu.Unlock()
							reportError(fmt.Errorf("error counting text token: %v", err))
							return
						}
						logger.LogInfo(c, fmt.Sprintf("type: %s, textToken: %d, audioToken: %d", realtimeEvent.Type, textToken, audioToken))
						localUsage.TotalTokens += textToken + audioToken
						info.IsFirstRequest = false
						localUsage.InputTokens += textToken + audioToken
						localUsage.InputTokenDetails.TextTokens += textToken
						localUsage.InputTokenDetails.AudioTokens += audioToken
						err = preConsumeUsage(c, info, localUsage, sumUsage)
						if err != nil {
							settlementFailed = true
							stateMu.Unlock()
							reportError(fmt.Errorf("error consume usage: %v", err))
							return
						}
						// 本次计费完成，清除
						localUsage = &dto.RealtimeUsage{}
						// print now usage
					}
					logger.LogInfo(c, fmt.Sprintf("realtime streaming sumUsage: %v", sumUsage))
					logger.LogInfo(c, fmt.Sprintf("realtime streaming localUsage: %v", localUsage))
					logger.LogInfo(c, fmt.Sprintf("realtime streaming localUsage: %v", localUsage))

				} else if realtimeEvent.Type == dto.RealtimeEventTypeSessionUpdated || realtimeEvent.Type == dto.RealtimeEventTypeSessionCreated {
					realtimeSession := realtimeEvent.Session
					if realtimeSession != nil {
						// update audio format
						info.InputAudioFormat = common.GetStringIfEmpty(realtimeSession.InputAudioFormat, info.InputAudioFormat)
						info.OutputAudioFormat = common.GetStringIfEmpty(realtimeSession.OutputAudioFormat, info.OutputAudioFormat)
					}
				} else {
					textToken, audioToken, err := service.CountTokenRealtime(info, *realtimeEvent, info.UpstreamModelName)
					if err != nil {
						stateMu.Unlock()
						reportError(fmt.Errorf("error counting text token: %v", err))
						return
					}
					logger.LogInfo(c, fmt.Sprintf("type: %s, textToken: %d, audioToken: %d", realtimeEvent.Type, textToken, audioToken))
					localUsage.TotalTokens += textToken + audioToken
					localUsage.OutputTokens += textToken + audioToken
					localUsage.OutputTokenDetails.TextTokens += textToken
					localUsage.OutputTokenDetails.AudioTokens += audioToken
				}
				stateMu.Unlock()

				err = helper.WssString(c, clientConn, string(message))
				if err != nil {
					reportError(fmt.Errorf("error writing to client: %v", err))
					return
				}
			}
		}
	})

	var relayErr error
	select {
	case <-readerDone:
	case relayErr = <-errChan:
	case <-c.Done():
	}

	// Closing both sockets interrupts the other reader before the final usage
	// snapshot is settled. Without this join, a reader can mutate usage after the
	// handler has returned and race the final billing step.
	_ = clientConn.Close()
	_ = targetConn.Close()
	readers.Wait()

	if relayErr != nil {
		logger.LogError(c, "realtime error: "+relayErr.Error())
	}

	if !settlementFailed && usage.TotalTokens != 0 {
		if err := preConsumeUsage(c, info, usage, sumUsage); err != nil {
			settlementFailed = true
			logger.LogError(c, "realtime final upstream settlement error: "+err.Error())
			if relayErr == nil {
				relayErr = fmt.Errorf("error consume final upstream usage: %w", err)
			}
		}
	}

	if !settlementFailed && localUsage.TotalTokens != 0 {
		if err := preConsumeUsage(c, info, localUsage, sumUsage); err != nil {
			logger.LogError(c, "realtime final local settlement error: "+err.Error())
			if relayErr == nil {
				relayErr = fmt.Errorf("error consume final local usage: %w", err)
			}
		}
	}

	if relayErr != nil {
		if service.IsUpstreamAccepted(c) {
			return channel.AcceptedResponseDeliveryError(), sumUsage
		}
		return types.NewError(relayErr, types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry()), sumUsage
	}

	return nil, sumUsage
}

func preConsumeUsage(ctx *gin.Context, info *relaycommon.RelayInfo, usage *dto.RealtimeUsage, totalUsage *dto.RealtimeUsage) error {
	if usage == nil || totalUsage == nil {
		return fmt.Errorf("invalid usage pointer")
	}

	if err := service.PreWssConsumeQuota(ctx, info, usage); err != nil {
		return err
	}

	totalUsage.TotalTokens += usage.TotalTokens
	totalUsage.InputTokens += usage.InputTokens
	totalUsage.OutputTokens += usage.OutputTokens
	totalUsage.InputTokenDetails.CachedTokens += usage.InputTokenDetails.CachedTokens
	totalUsage.InputTokenDetails.TextTokens += usage.InputTokenDetails.TextTokens
	totalUsage.InputTokenDetails.AudioTokens += usage.InputTokenDetails.AudioTokens
	totalUsage.OutputTokenDetails.TextTokens += usage.OutputTokenDetails.TextTokens
	totalUsage.OutputTokenDetails.AudioTokens += usage.OutputTokenDetails.AudioTokens
	return nil
}
