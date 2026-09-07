<?php

namespace QueueGuard;

/**
 * QueueGuard PHP Middleware & Ticket Validator
 * Zero external dependencies. Compatible with PHP 7.4+ and PHP 8+.
 */
class Validator
{
    /**
     * Compute SHA256 device fingerprint from user-agent and client IP.
     */
    public static function computeDeviceFingerprint(string $userAgent = '', string $clientIp = ''): string
    {
        if (empty($userAgent) && empty($clientIp)) {
            return '';
        }
        return substr(hash('sha256', $userAgent . '|' . $clientIp), 0, 16);
    }

    /**
     * Decode base64url string.
     */
    public static function base64UrlDecode(string $data): string
    {
        $remainder = strlen($data) % 4;
        if ($remainder) {
            $padlen = 4 - $remainder;
            $data .= str_repeat('=', $padlen);
        }
        return base64_decode(strtr($data, '-_', '+/'));
    }

    /**
     * Verify a QueueGuard admission ticket token.
     *
     * @param string $token Token string in format "payload.signature"
     * @param string $secretKey Shared secret key
     * @param array $options Verification options (expectedDeviceHash, expectedRoomId)
     * @return array Decoded ticket payload
     * @throws \Exception If verification fails
     */
    public static function verifyTicket(string $token, string $secretKey, array $options = []): array
    {
        $parts = explode('.', $token);
        if (count($parts) !== 2) {
            throw new \InvalidArgumentException('Invalid ticket format');
        }

        [$payloadB64, $signature] = $parts;

        // Verify HMAC-SHA256 signature
        $expectedSignature = hash_hmac('sha256', $payloadB64, $secretKey);
        if (!hash_equals($expectedSignature, $signature)) {
            throw new \SecurityException('Ticket signature verification failed');
        }

        // Decode JSON payload
        $payloadJson = self::base64UrlDecode($payloadB64);
        $ticket = json_decode($payloadJson, true);
        if (!is_array($ticket)) {
            throw new \InvalidArgumentException('Malformed ticket JSON payload');
        }

        // Check Expiration (Unix timestamp in milliseconds)
        $nowMilli = (int)(microtime(true) * 1000);
        if (isset($ticket['exp']) && $nowMilli > $ticket['exp']) {
            throw new \RuntimeException('Ticket has expired');
        }

        // Check Device Binding Fingerprint
        if (!empty($options['expectedDeviceHash']) && !empty($ticket['dev'])) {
            if ($ticket['dev'] !== $options['expectedDeviceHash']) {
                throw new \SecurityException('Ticket device fingerprint mismatch');
            }
        }

        // Check Room ID
        if (!empty($options['expectedRoomId']) && !empty($ticket['rid'])) {
            if ($ticket['rid'] !== $options['expectedRoomId']) {
                throw new \SecurityException('Ticket room ID mismatch');
            }
        }

        return $ticket;
    }

    /**
     * Inspect incoming HTTP request in pure PHP / Laravel / Symfony.
     */
    public static function protect(string $secretKey, array $options = []): array
    {
        $cookieName = $options['ticketCookie'] ?? 'queueguard_ticket';
        $headerName = 'HTTP_' . strtoupper(str_replace('-', '_', $options['ticketHeader'] ?? 'X-QueueGuard-Ticket'));

        $token = $_COOKIE[$cookieName] ?? $_SERVER[$headerName] ?? null;

        if (!$token) {
            self::denyAccess($options['redirectUrl'] ?? null);
        }

        $expectedDeviceHash = '';
        if ($options['bindDevice'] ?? true) {
            $ua = $_SERVER['HTTP_USER_AGENT'] ?? '';
            $ip = $_SERVER['HTTP_X_FORWARDED_FOR'] ?? $_SERVER['REMOTE_ADDR'] ?? '';
            if (strpos($ip, ',') !== false) {
                $ip = trim(explode(',', $ip)[0]);
            }
            $expectedDeviceHash = self::computeDeviceFingerprint($ua, $ip);
        }

        try {
            return self::verifyTicket($token, $secretKey, [
                'expectedDeviceHash' => $expectedDeviceHash,
                'expectedRoomId' => $options['roomId'] ?? '',
            ]);
        } catch (\Throwable $e) {
            self::denyAccess($options['redirectUrl'] ?? null);
        }
    }

    protected static function denyAccess(?string $redirectUrl): void
    {
        if (!empty($redirectUrl)) {
            header('Location: ' . $redirectUrl, true, 302);
            exit;
        }

        http_response_code(401);
        header('Content-Type: application/json');
        echo json_encode([
            'error' => 'unauthorized',
            'message' => 'Valid QueueGuard admission ticket required',
        ]);
        exit;
    }
}
