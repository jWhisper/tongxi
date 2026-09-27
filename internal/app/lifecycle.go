package app

import (
	"errors"
	"time"
	"tongxi/internal/store"
)

func (s *Service) StopChain(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopChainLocked(id)
}
func (s *Service) stopChainLocked(id string) error {
	changed, err := s.db.StopChain(id, s.chatActive)
	if err != nil {
		// A disk failure must not keep the provider running or permit more tools.
		if s.chatActive != nil && s.chatActive.ChainID == id && s.chatCancel != nil {
			s.chatCancel()
			s.chatActive.Status, s.chatActive.Error = "cancelled", "已请求停止，但状态未能保存，请检查磁盘空间"
			s.chatActive.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
			s.chatActive.Revision++
		}
		return workspaceError(err)
	}
	s.applyCancelled(changed)
	s.wakeQueue()
	return nil
}

// Called under the service lock, so model updates cannot race past cancellation.
func (s *Service) applyCancelled(changed []store.ConversationRun) {
	for _, r := range changed {
		if s.chatActive != nil && s.chatActive.ID == r.ID {
			copy := cloneChat(r)
			s.chatActive = &copy
			if s.chatCancel != nil {
				s.chatCancel()
			}
		}
		if s.chatEmit != nil {
			s.chatEmit(cloneChat(r))
		}
	}
}
func (s *Service) RetryRun(id, requestID string) (store.ConversationRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return store.ConversationRun{}, errors.New("应用正在退出")
	}
	if len(requestID) < 16 || len(requestID) > 128 {
		return store.ConversationRun{}, errors.New("无效的重试标识")
	}
	r, err := s.db.RetryRun(id, requestID, newID())
	if err == nil {
		s.wakeQueue()
	}
	return r, workspaceError(err)
}
