# 🌐 QueueGuard Cloudflare Edge Worker

Xác thực vé vào cổng trực tiếp tại hơn 300 trạm CDN Edge toàn cầu của Cloudflare.

- Sử dụng **Web Crypto API** (`crypto.subtle`) cho độ trễ xác thực cực thấp (< 1ms).
- **Chặn đứng 100% lưu lượng chưa có vé** ngay tại Edge, triệt tiêu hoàn toàn nguy cơ sập Origin Backend hoặc Database.
- Tự động cho qua các tệp tĩnh (`.css`, `.js`, ảnh) và webhook.

---

## 🚀 Triển khai (Deploy)

1. Cài đặt Cloudflare Wrangler CLI:
   ```bash
   npm install -g wrangler
   ```

2. Cấu hình biến môi trường trong `wrangler.toml`:
   - `QUEUEGUARD_URL`: Địa chỉ của máy chủ QueueGuard (nơi đón khách chưa có vé).
   - `SECRET_KEY`: Khóa bí mật chung dùng để ký vé HMAC-SHA256.

3. Triển khai lên Cloudflare:
   ```bash
   wrangler deploy
   ```
