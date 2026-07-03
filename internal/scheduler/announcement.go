package scheduler

import (
	"context"
	"fmt"
	"time"
)

const (
	systemAnnouncementName     = "系统"
	systemAnnouncementSenderID = uint16(1)
	systemAnnouncementInterval = 5 * time.Minute

	SystemAnnouncementMegaphone       = "megaphone"
	SystemAnnouncementWebNoticeSingle = "web_notice_single"
)

type AnnouncementResult struct {
	Online       int       `json:"online"`
	AuctionKinds int       `json:"auction_kinds"`
	Kind         string    `json:"kind"`
	Message      string    `json:"message"`
	Sent         bool      `json:"sent"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (m *RobotManager) SystemAnnouncement() (AnnouncementResult, error) {
	return m.SystemAnnouncementAs(SystemAnnouncementWebNoticeSingle)
}

func (m *RobotManager) SystemAnnouncementAs(kind string) (AnnouncementResult, error) {
	return m.MonitorAnnouncement(kind, "")
}

func (m *RobotManager) MonitorAnnouncement(kind, message string) (AnnouncementResult, error) {
	now := time.Now()
	online, err := m.systemOnlineCount()
	if err != nil {
		return AnnouncementResult{Kind: kind, UpdatedAt: now}, err
	}
	auctionKinds, err := m.systemAuctionKindCount()
	if err != nil {
		return AnnouncementResult{Online: online, Kind: kind, UpdatedAt: now}, err
	}
	msg := SystemAnnouncementMessageAt(now, online, auctionKinds)
	if message != "" {
		msg = message
	}
	res := AnnouncementResult{
		Online:       online,
		AuctionKinds: auctionKinds,
		Kind:         kind,
		Message:      msg,
		UpdatedAt:    now,
	}
	if err := m.worldShout.SendMonitorAnnouncement(kind, msg, systemAnnouncementName, systemAnnouncementSenderID); err != nil {
		return res, err
	}
	res.Sent = true
	return res, nil
}

func (s *RobotSupervisor) sendSystemAnnouncementIfDue(now time.Time) {
	if s.nextAnnouncement.IsZero() {
		s.nextAnnouncement = now.Add(systemAnnouncementInterval)
		return
	}
	if now.Before(s.nextAnnouncement) {
		return
	}
	s.nextAnnouncement = now.Add(systemAnnouncementInterval)
	res, err := s.manager.SystemAnnouncement()
	if err != nil {
		robotLogf("[Announcement] system failed online=%d auction_kinds=%d err=%v\n", res.Online, res.AuctionKinds, err)
		return
	}
	robotLogf("[Announcement] system sent online=%d auction_kinds=%d message=%s\n", res.Online, res.AuctionKinds, res.Message)
}

func SystemAnnouncementMessage(online int) string {
	return SystemAnnouncementMessageAt(time.Now(), online, 0)
}

func SystemAnnouncementMessageAt(now time.Time, online, auctionKinds int) string {
	if online < 0 {
		online = 0
	}
	if auctionKinds < 0 {
		auctionKinds = 0
	}
	return fmt.Sprintf("%s 在线人数%d；拍卖行%d类", now.Format("15:04:05"), online, auctionKinds)
}

func (m *RobotManager) systemOnlineCount() (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var online int
	err := m.database.QueryRowContext(ctx, "SELECT COUNT(*) FROM taiwan_login.login_account_3 WHERE login_status=1").Scan(&online)
	if err != nil {
		return 0, fmt.Errorf("query system online count: %w", err)
	}
	if online < 0 {
		online = 0
	}
	return online, nil
}

func (m *RobotManager) systemAuctionKindCount() (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var kinds int
	err := m.database.QueryRowContext(ctx, "SELECT COUNT(DISTINCT item_id) FROM taiwan_cain_auction_gold.auction_main").Scan(&kinds)
	if err != nil {
		return 0, fmt.Errorf("query auction kind count: %w", err)
	}
	if kinds < 0 {
		kinds = 0
	}
	return kinds, nil
}
