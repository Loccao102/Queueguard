# 🚀 @queueguard/middleware-node

Middleware xác thực vé vào cổng **QueueGuard** không phụ thuộc thư viện ngoài (**Zero Dependencies**) cho **Node.js** và **Express.js**.

- Sử dụng module `crypto` có sẵn của Node.js.
- Xác thực chữ ký số **HMAC-SHA256**, hạn sử dụng **TTL**, **Fingerprint thiết bị**, và **Phân khu phòng chờ (Room ID)**.

---

## 💻 Cài đặt & Sử dụng

```javascript
const express = require('express');
const { queueguardMiddleware } = require('./sdk/middleware/nodejs');

const app = express();

// Bảo vệ toàn bộ hoặc từng route
app.use('/checkout', queueguardMiddleware({
  secretKey: process.env.SECRET_KEY || 'queueguard-secret-key',
  roomId: 'vip',              // Tùy chọn: bắt buộc vé thuộc phòng VIP
  bindDevice: true,           // Khóa vé theo User-Agent và IP
  redirectUrl: 'https://queue.example.com/tickets/vip', // Tùy chọn redirect nếu chưa có vé
}));

app.get('/checkout', (req, res) => {
  // Lấy thông tin vé đã xác thực từ context request
  const { ticket, sessionId, roomId } = req.queueguard;
  res.json({
    message: 'Chào mừng bạn đã vào trang thanh toán!',
    ticketNumber: ticket.qnum,
    sessionId,
    roomId,
  });
});

app.listen(8080);
```

---

## ⚙️ Các Tham Số Cấu Hình

| Thuộc tính | Kiểu dữ liệu | Mặc định | Ý nghĩa |
| :--- | :--- | :--- | :--- |
| `secretKey` | `string` | **Bắt buộc** | Khóa bí mật dùng để xác thực HMAC-SHA256 |
| `roomId` | `string` | `""` | ID phòng chờ bắt buộc (nếu áp dụng đa phòng chờ) |
| `bindDevice` | `boolean` | `true` | Ràng buộc vé theo thiết bị client (User-Agent + IP) |
| `redirectUrl` | `string` | `""` | URL chuyển hướng tới phòng chờ nếu vé không hợp lệ |
| `ticketCookie` | `string` | `"queueguard_ticket"` | Tên Cookie chứa vé vào cổng |
| `ticketHeader` | `string` | `"x-queueguard-ticket"` | Tên Header thay thế Cookie |
