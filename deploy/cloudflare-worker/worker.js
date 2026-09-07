/**
 * QueueGuard CDN Edge Verification Worker
 * Runs on Cloudflare Workers / Fastly Compute / Vercel Edge.
 * Validates HMAC-SHA256 admission tickets at Edge locations worldwide (< 1ms).
 */

export default {
  async fetch(request, env, ctx) {
    const url = new URL(request.url);

    // 1. Static asset & bypass paths pass directly through to Origin
    if (isBypassPath(url.pathname)) {
      return fetch(request);
    }

    // 2. Extract admission ticket cookie or header
    const cookieHeader = request.headers.get('Cookie') || '';
    const ticketCookie = parseCookie(cookieHeader, 'queueguard_ticket');
    const ticketToken = ticketCookie || request.headers.get('X-QueueGuard-Ticket');

    if (!ticketToken) {
      return handleUnadmitted(request, env);
    }

    // 3. Cryptographic Verification using Web Crypto API
    const secretKey = env.SECRET_KEY || 'queueguard-dev-secret-key-change-me';
    const isValid = await verifyTicket(ticketToken, secretKey);

    if (!isValid) {
      return handleUnadmitted(request, env);
    }

    // 4. Ticket is valid! Forward request directly to Origin Server
    const response = await fetch(request);
    return response;
  }
};

/**
 * Verifies HMAC-SHA256 token using Edge Web Crypto API.
 */
async function verifyTicket(tokenStr, secretKey) {
  try {
    const parts = tokenStr.split('.');
    if (parts.length !== 2) return false;

    const [payloadB64, signatureHex] = parts;

    // Import secret key
    const encoder = new TextEncoder();
    const key = await crypto.subtle.importKey(
      'raw',
      encoder.encode(secretKey),
      { name: 'HMAC', hash: 'SHA-256' },
      false,
      ['sign']
    );

    // Compute signature over payload
    const signatureBuffer = await crypto.subtle.sign(
      'HMAC',
      key,
      encoder.encode(payloadB64)
    );

    const computedSigHex = buf2hex(signatureBuffer);
    if (computedSigHex !== signatureHex) {
      return false;
    }

    // Decode and parse JSON payload
    const payloadJson = atob(payloadB64.replace(/-/g, '+').replace(/_/g, '/'));
    const ticket = JSON.parse(payloadJson);

    // Check expiration timestamp
    if (Date.now() > ticket.exp) {
      return false;
    }

    return true;
  } catch (err) {
    return false;
  }
}

function handleUnadmitted(request, env) {
  const queueGuardUrl = env.QUEUEGUARD_URL || 'https://queue.example.com';
  const url = new URL(request.url);

  // Return redirect to QueueGuard Waiting Room
  const redirectUrl = new URL(queueGuardUrl);
  redirectUrl.searchParams.set('target', url.pathname + url.search);

  return Response.redirect(redirectUrl.toString(), 302);
}

function isBypassPath(pathname) {
  const extensions = ['.css', '.js', '.png', '.jpg', '.jpeg', '.svg', '.ico', '.woff2', '.webp'];
  for (const ext of extensions) {
    if (pathname.endsWith(ext)) return true;
  }
  if (pathname === '/healthz' || pathname.startsWith('/api/webhooks/')) {
    return true;
  }
  return false;
}

function parseCookie(cookieStr, key) {
  const match = cookieStr.match(new RegExp('(^|;\\s*)(' + key + ')=([^;]*)'));
  return match ? decodeURIComponent(match[3]) : null;
}

function buf2hex(buffer) {
  return [...new Uint8Array(buffer)]
    .map(x => x.toString(16).padStart(2, '0'))
    .join('');
}
