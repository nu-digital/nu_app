package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nu-digital/nu_app/services/api/internal/authz"
	"github.com/nu-digital/nu_app/services/api/internal/httpx"
	"github.com/nu-digital/nu_app/services/api/internal/notify"
	"github.com/nu-digital/nu_app/services/api/internal/store"
)

// 通知イベント種別。notification_settings のCHECK制約と一致させる。
const (
	EventLoanCreated       = "inventory.loan_created"
	EventLoanReturned      = "inventory.loan_returned"
	EventCorrectionCreated = "inventory.correction_created"
	EventContentPublished  = "content.published"
)

// NotificationPayload は通知本文の材料。**機密を入れない**（学生証番号・大学メール・氏名・最寄り駅・token・訂正の理由）。
// 表示名は他のメンバーにも見える項目だけ。Version は本文の形式を将来変えるための番号。
type NotificationPayload struct {
	Version   int    `json:"v"`
	ItemName  string `json:"itemName"`
	AssetTag  string `json:"assetTag,omitempty"`
	Quantity  int    `json:"quantity,omitempty"`
	Borrower  string `json:"borrower,omitempty"`
	Actor     string `json:"actor,omitempty"`
	Proxy     bool   `json:"proxy,omitempty"`
	Remaining *int64 `json:"remaining,omitempty"`
	Kind      string `json:"kind,omitempty"`   // 訂正: void / adjust
	Target    string `json:"target,omitempty"` // 訂正: loan / return
	Delta     int    `json:"delta,omitempty"`
}

// enqueueNotification は業務のトランザクションの中でoutboxへ積む。設定が無効なら積まない。
// 積めなければ（DB障害）呼び出し元のトランザクションごと巻き戻る＝業務確定とoutboxは常に一致する。
// Discordへの送信はworkerが別に行うため、Discordの障害は業務操作に影響しない。
func (s *server) enqueueNotification(ctx context.Context, q *store.Queries, eventType, dedupeKey string, p NotificationPayload) error {
	p.Version = 1
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = q.EnqueueNotification(ctx, store.EnqueueNotificationParams{EventType: eventType, DedupeKey: dedupeKey, PayloadJson: b})
	return err
}

func (s *server) displayName(ctx context.Context, q *store.Queries, id pgtype.UUID) string {
	n, err := q.GetDisplayName(ctx, id)
	if err != nil {
		return "（不明）"
	}
	return n
}

var channelIDPattern = regexp.MustCompile(`^[0-9]{5,25}$`)

type settingView struct {
	EventType        string    `json:"eventType"`
	Label            string    `json:"label"`
	Enabled          bool      `json:"enabled"`
	DiscordChannelID *string   `json:"discordChannelId"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

var eventLabels = map[string]string{
	EventLoanCreated:       "物品の貸出",
	EventLoanReturned:      "物品の返却",
	EventCorrectionCreated: "物品の記録の訂正",
	EventContentPublished:  "記事・作品の公開",
}

func toSettingView(r store.NotificationSetting) settingView {
	v := settingView{EventType: r.EventType, Label: eventLabels[r.EventType], Enabled: r.Enabled, UpdatedAt: r.UpdatedAt.Time}
	if r.DiscordChannelID.Valid {
		c := r.DiscordChannelID.String
		v.DiscordChannelID = &c
	}
	return v
}

// listNotificationSettings は GET /admin/notifications/settings。
func (s *server) listNotificationSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorize(w, r, "notifications.settings.list", authz.Resource{}); !ok {
		return
	}
	rows, err := store.New(s.Pool).ListNotificationSettings(r.Context())
	if err != nil {
		s.internal(w, r)
		return
	}
	out := make([]settingView, 0, len(rows))
	for _, row := range rows {
		out = append(out, toSettingView(row))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": out})
}

// putNotificationSetting は PUT /admin/notifications/settings/{eventType}（Admin/Owner）。
// ON/OFFと送信チャンネルを変える。チャンネルIDは数字だけ（Discordのsnowflake）。空/nullで未設定に戻す。
func (s *server) putNotificationSetting(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "notifications.settings.update", authz.Resource{})
	if !ok {
		return
	}
	eventType := r.PathValue("eventType")
	var req struct {
		Enabled          *bool   `json:"enabled"`
		DiscordChannelID *string `json:"discordChannelId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Enabled == nil {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "有効/無効を指定してください")
		return
	}
	channel := pgtype.Text{}
	if req.DiscordChannelID != nil && *req.DiscordChannelID != "" {
		if !channelIDPattern.MatchString(*req.DiscordChannelID) {
			httpx.Error(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "チャンネルIDは数字だけで入力してください")
			return
		}
		channel = pgtype.Text{String: *req.DiscordChannelID, Valid: true}
	}
	var out settingView
	err := s.inTx(r.Context(), func(q *store.Queries, _ pgx.Tx) error {
		if _, err := q.GetNotificationSetting(r.Context(), eventType); err != nil {
			return &errTx{http.StatusNotFound, "NOT_FOUND", "見つかりません"}
		}
		row, err := q.UpdateNotificationSetting(r.Context(), store.UpdateNotificationSettingParams{EventType: eventType, Enabled: *req.Enabled, DiscordChannelID: channel, UpdatedBy: a.Identity.UserID})
		if err != nil {
			return err
		}
		out = toSettingView(row)
		// チャンネルIDは識別子で秘密ではないが、監査ログには変更の事実だけを残す
		return s.auditEvent(r.Context(), q, a.Identity.UserID, a.Identity.UserID, "notifications.setting_updated", map[string]any{"eventType": eventType, "enabled": *req.Enabled, "channelSet": channel.Valid})
	})
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

type deliveryView struct {
	ID            string     `json:"id"`
	EventType     string     `json:"eventType"`
	Label         string     `json:"label"`
	Status        string     `json:"status"`
	Attempts      int32      `json:"attempts"`
	NextAttemptAt time.Time  `json:"nextAttemptAt"`
	LastError     *string    `json:"lastError"`
	CreatedAt     time.Time  `json:"createdAt"`
	SentAt        *time.Time `json:"sentAt"`
}

// listNotificationDeliveries は GET /admin/notifications/deliveries。本文（payload）は返さない。
func (s *server) listNotificationDeliveries(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorize(w, r, "notifications.deliveries.list", authz.Resource{}); !ok {
		return
	}
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 {
			httpx.Error(w, r, http.StatusBadRequest, "INVALID_REQUEST", "limit は1〜100で指定してください")
			return
		}
		limit = n
	}
	status := pgtype.Text{}
	if v := r.URL.Query().Get("status"); v != "" {
		if v != "pending" && v != "sent" && v != "dead" {
			httpx.Error(w, r, http.StatusBadRequest, "INVALID_REQUEST", "status が正しくありません")
			return
		}
		status = pgtype.Text{String: v, Valid: true}
	}
	rows, err := store.New(s.Pool).ListNotificationDeliveries(r.Context(), store.ListNotificationDeliveriesParams{Limit: int32(limit), Status: status})
	if err != nil {
		s.internal(w, r)
		return
	}
	out := make([]deliveryView, 0, len(rows))
	for _, d := range rows {
		v := deliveryView{ID: d.ID.String(), EventType: d.EventType, Label: eventLabels[d.EventType], Status: d.Status, Attempts: d.Attempts, NextAttemptAt: d.NextAttemptAt.Time, CreatedAt: d.CreatedAt.Time}
		if d.LastError.Valid {
			e := d.LastError.String
			v.LastError = &e
		}
		if d.SentAt.Valid {
			t := d.SentAt.Time
			v.SentAt = &t
		}
		out = append(out, v)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": out})
}

// retryNotificationDelivery は POST /admin/notifications/deliveries/{id}/retry。送信を諦めたもの(dead)だけを再送待ちに戻す。
func (s *server) retryNotificationDelivery(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "notifications.deliveries.retry", authz.Resource{})
	if !ok {
		return
	}
	id, ok := parseItemID(w, r)
	if !ok {
		return
	}
	err := s.inTx(r.Context(), func(q *store.Queries, _ pgx.Tx) error {
		n, err := q.RetryNotificationDelivery(r.Context(), id)
		if err != nil {
			return err
		}
		if n != 1 {
			return &errTx{http.StatusConflict, "NOT_RETRYABLE", "再送できるのは、送信に失敗して止まっているものだけです"}
		}
		return s.auditEvent(r.Context(), q, a.Identity.UserID, a.Identity.UserID, "notifications.delivery_retried", map[string]any{"deliveryId": id.String()})
	})
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// testNotification は POST /admin/notifications/test（Admin/Owner）。指定したチャンネルへ接続テストの1通を送る。
// Botのtoken・権限・チャンネルIDが正しいかを、本番の通知を待たずに確かめる。結果は成否と短い要約だけ（応答本文・tokenは返さない）。
func (s *server) testNotification(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "notifications.test", authz.Resource{})
	if !ok {
		return
	}
	var req struct {
		DiscordChannelID string `json:"discordChannelId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if !channelIDPattern.MatchString(req.DiscordChannelID) {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "チャンネルIDは数字だけで入力してください")
		return
	}
	if s.Notifier == nil {
		httpx.Error(w, r, http.StatusServiceUnavailable, "BOT_NOT_CONFIGURED", "Discord Botが設定されていません（サーバーの設定でtokenを指定してください）")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	err := s.Notifier.Send(ctx, req.DiscordChannelID, "【接続テスト】nu_appからの通知テストです。このメッセージは無視してください。")
	if aerr := s.auditEvent(r.Context(), store.New(s.Pool), a.Identity.UserID, a.Identity.UserID, "notifications.test_sent", map[string]any{"ok": err == nil}); aerr != nil {
		s.internal(w, r)
		return
	}
	if err != nil {
		var se *notify.SendError
		code, status, msg := "DISCORD_UNAVAILABLE", http.StatusBadGateway, "Discordへ送信できませんでした"
		if errors.As(err, &se) {
			msg = se.Summary
			switch se.Kind {
			case notify.KindPermanent:
				code = "DISCORD_REJECTED"
			case notify.KindRateLimited:
				code, status = "DISCORD_RATE_LIMITED", http.StatusTooManyRequests
			}
		}
		httpx.Error(w, r, status, code, msg)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}
