# ☁️ QueueGuard Helm Chart

Triển khai QueueGuard Virtual Waiting Room lên Kubernetes (EKS, GKE, AKS, Minikube, K3s).

## Cài đặt nhanh

```bash
# Thêm cấu hình giá trị tùy chỉnh nếu cần
helm install queueguard ./deploy/helm/queueguard \
  --set config.originUrl="http://my-backend-service:8080" \
  --set config.dischargeRate=30 \
  --set autoscaling.enabled=true
```

## Các tham số cấu hình chính

| Tham số | Mặc định | Mô tả |
| :--- | :--- | :--- |
| `replicaCount` | `2` | Số lượng pod chạy song song |
| `autoscaling.enabled` | `true` | Bật HPA tự động scale theo CPU/RAM |
| `autoscaling.maxReplicas` | `20` | Số lượng pod tối đa khi quá tải |
| `config.originUrl` | `http://protected-origin:8080` | Địa chỉ backend cần bảo vệ |
| `config.dischargeRate` | `20` | Số lượng user xả vào web mỗi giây |
| `config.bindDevice` | `true` | Ràng buộc vé với fingerprint thiết bị |
| `config.powDifficulty` | `0` | Thử thách giải toán chống botnet |
| `config.roomsConfig` | `""` | Cấu hình đa phòng chờ (id:prefix:rate) |
| `config.themeColor` | `#06b6d4` | Màu sắc chủ đạo giao diện phòng chờ |

