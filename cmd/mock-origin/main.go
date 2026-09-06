package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync/atomic"
	"time"
)

var (
	totalRequests uint64
	ticketsBought uint64
	stockLeft     int64 = 100
)

func main() {
	port := os.Getenv("MOCK_PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()

	// Landing / Checkout page
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddUint64(&totalRequests, 1)

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
  <meta charset="UTF-8">
  <title>🔥 Super Concert Flash Sale 2026</title>
  <style>
    body { font-family: sans-serif; background: #0b0f19; color: #fff; text-align: center; padding: 4rem 1rem; }
    .card { background: #161f30; max-width: 480px; margin: auto; padding: 2rem; border-radius: 12px; border: 1px solid #2a3b5c; }
    h1 { color: #38bdf8; }
    .btn { background: #10b981; color: #fff; padding: 12px 24px; border: none; border-radius: 6px; font-weight: bold; cursor: pointer; font-size: 1.1rem; }
    .btn:hover { background: #059669; }
    .stock { font-size: 1.5rem; font-weight: bold; color: #f59e0b; margin: 1.5rem 0; }
  </style>
</head>
<body>
  <div class="card">
    <h1>🎉 CHÚC MỪNG BẠN ĐÃ VÀO THÀNH CÔNG!</h1>
    <p>Bạn đã vượt qua hàng chờ QueueGuard an toàn và được cấp vé truy cập hệ thống.</p>
    <div class="stock">Vé còn lại: <span id="stock">%d</span> / 100</div>
    <form action="/buy" method="POST">
      <button class="btn" type="submit">🎟️ Mua Vé Ngay</button>
    </form>
    <p style="margin-top: 1.5rem; font-size: 0.8rem; color: #94a3b8;">Tổng lượt truy cập hợp lệ đã vào origin: %d</p>
  </div>
</body>
</html>`, atomic.LoadInt64(&stockLeft), atomic.LoadUint64(&totalRequests))
	})

	// Buy action
	mux.HandleFunc("/buy", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddUint64(&totalRequests, 1)

		if atomic.LoadInt64(&stockLeft) <= 0 {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintln(w, "❌ RẤT TIẾC: Vé đã bán hết sạch!")
			return
		}

		atomic.AddInt64(&stockLeft, -1)
		bought := atomic.AddUint64(&ticketsBought, 1)

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
  <meta charset="UTF-8">
  <title>Mua Vé Thành Công</title>
  <style>
    body { font-family: sans-serif; background: #0b0f19; color: #fff; text-align: center; padding: 4rem 1rem; }
    .card { background: #161f30; max-width: 480px; margin: auto; padding: 2rem; border-radius: 12px; border: 1px solid #10b981; }
    h1 { color: #10b981; }
  </style>
</head>
<body>
  <div class="card">
    <h1>✅ ĐẶT VÉ THÀNH CÔNG!</h1>
    <p>Mã đặt chỗ của bạn: #CONCERT-%04d</p>
    <p>Thời gian giao dịch: %s</p>
    <br/>
    <a href="/" style="color: #38bdf8;">← Quay lại</a>
  </div>
</body>
</html>`, bought, time.Now().Format("15:04:05 02/01/2006"))
	})

	// API metric endpoint
	mux.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total_requests": atomic.LoadUint64(&totalRequests),
			"tickets_bought": atomic.LoadUint64(&ticketsBought),
			"stock_left":     atomic.LoadInt64(&stockLeft),
		})
	})

	log.Printf("🚀 Mock Origin Target Server running on http://localhost:%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("Mock origin server failed: %v", err)
	}
}
