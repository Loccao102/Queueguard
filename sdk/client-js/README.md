# 📦 @queueguard/client

Thư viện JavaScript / TypeScript chính thức kết nối phòng chờ ảo **QueueGuard**.

- **Zero dependencies** (sử dụng API chuẩn của trình duyệt).
- Hỗ trợ cả **Vanilla JavaScript** và **React Hook (`useQueueGuard`)**.
- Tích hợp sẵn chuông báo âm thanh Web Audio API và Web Push Notifications.

---

## 🚀 Cài đặt

```bash
npm install @queueguard/client
# hoặc
pnpm add @queueguard/client
# hoặc
yarn add @queueguard/client
```

---

## 💻 Sử dụng với React / Next.js

```tsx
import React from 'react';
import { useQueueGuard } from '@queueguard/client';

export function WaitingRoomComponent() {
  const {
    position,
    estSeconds,
    ticketNumber,
    isAdmitted,
    isPreQueue,
    roomName,
    requestNotification,
  } = useQueueGuard({
    roomId: 'vip',
    autoRedirect: true,
  });

  if (isAdmitted) {
    return <div>🎉 Đến lượt bạn! Đang chuyển hướng...</div>;
  }

  return (
    <div className="waiting-box">
      <h2>{roomName || 'Đang trong hàng chờ'}</h2>
      {isPreQueue ? (
        <p>Sự kiện mở sau: {estSeconds}s</p>
      ) : (
        <>
          <p>Số người phía trước: {position}</p>
          <p>Thời gian ước tính: {estSeconds} giây</p>
          <p>Mã vé: #{ticketNumber}</p>
        </>
      )}
      <button onClick={requestNotification}>🔔 Nhận thông báo khi đến lượt</button>
    </div>
  );
}
```

---

## 🌐 Sử dụng với Vanilla JavaScript

```html
<script type="module">
  import { QueueGuardClient } from '@queueguard/client';

  const client = new QueueGuardClient({
    roomId: 'general',
    onUpdate: (data) => {
      console.log('Position ahead:', data.position);
      document.getElementById('pos').innerText = data.position;
    },
    onAdmitted: (data) => {
      alert('Đã đến lượt bạn!');
    }
  });

  client.connect();
</script>
```
