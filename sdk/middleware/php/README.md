# 🐘 QueueGuard PHP SDK & Middleware

Bộ thư viện xác thực vé vào cổng **QueueGuard** độc lập cho **PHP** (tương thích Laravel, Symfony, WordPress, thuần PHP).

- **Zero dependencies**: Chỉ sử dụng hàm `hash_hmac` và `json_decode` có sẵn của PHP.
- Xác thực chữ ký số **HMAC-SHA256**, thời hạn tự hủy **TTL**, **Fingerprint thiết bị** và **Phân khu phòng chờ (Room ID)**.

---

## 💻 Cách Sử Dụng Trong PHP

### Cách 1: Sử dụng trong file PHP thuần hoặc Controller

```php
<?php
require_once __DIR__ . '/sdk/middleware/php/QueueGuard.php';

use QueueGuard\Validator;

// Bảo vệ endpoint thanh toán: tự động chặn hoặc redirect nếu vé sai/hết hạn
$ticket = Validator::protect('queueguard-dev-secret-key-change-me', [
    'roomId'      => 'vip',                               // Bắt buộc vé thuộc phòng VIP
    'bindDevice'  => true,                                // Chống chuyển nhượng vé
    'redirectUrl' => 'https://queue.example.com/vip-room' // Tùy chọn redirect về phòng chờ
]);

// Nếu request qua được, biến $ticket chứa đầy đủ payload hợp lệ
echo "Xin chào phiên " . htmlspecialchars($ticket['sid']) . ", vé số #" . $ticket['qnum'];
```

### Cách 2: Tích hợp vào Laravel Middleware

```php
namespace App\Http\Middleware;

use Closure;
use QueueGuard\Validator;

class EnsureValidQueueGuardTicket
{
    public function handle($request, Closure $next)
    {
        $ticket = Validator::protect(config('services.queueguard.secret'), [
            'roomId'      => 'vip',
            'bindDevice'  => true,
            'redirectUrl' => route('waiting-room'),
        ]);

        $request->attributes->set('queueguard_ticket', $ticket);

        return $next($request);
    }
}
```
