/**
 * QueueGuard Node.js Middleware SDK
 * Zero-dependency verification of QueueGuard HMAC-SHA256 admission tickets.
 */

const crypto = require('crypto');

/**
 * Computes SHA256 device fingerprint from user-agent and client IP.
 */
function computeDeviceFingerprint(userAgent = '', clientIp = '') {
  if (!userAgent && !clientIp) return '';
  return crypto.createHash('sha256').update(`${userAgent}|${clientIp}`).digest('hex').substring(0, 16);
}

/**
 * Parses cookies from request header.
 */
function parseCookies(cookieHeader = '') {
  const list = {};
  cookieHeader.split(';').forEach((cookie) => {
    const parts = cookie.split('=');
    if (parts.length >= 2) {
      list[parts.shift().trim()] = decodeURIComponent(parts.join('='));
    }
  });
  return list;
}

/**
 * Verifies a QueueGuard ticket string.
 */
function verifyTicket(tokenStr, secretKey, options = {}) {
  if (!tokenStr || typeof tokenStr !== 'string') {
    throw new Error('Invalid ticket token format');
  }

  const parts = tokenStr.split('.');
  if (parts.length !== 2) {
    throw new Error('Invalid ticket token format');
  }

  const [payloadB64, signature] = parts;

  // Verify HMAC-SHA256 signature
  const expectedSig = crypto.createHmac('sha256', secretKey).update(payloadB64).digest('hex');
  if (!crypto.timingSafeEqual(Buffer.from(signature), Buffer.from(expectedSig))) {
    throw new Error('Ticket signature verification failed');
  }

  // Base64URL decode
  const payloadJson = Buffer.from(payloadB64, 'base64url').toString('utf8');
  const ticket = JSON.parse(payloadJson);

  // Check Expiry
  if (Date.now() > ticket.exp) {
    throw new Error('Ticket has expired');
  }

  // Check Device Fingerprint
  if (options.expectedDeviceHash && ticket.dev && ticket.dev !== options.expectedDeviceHash) {
    throw new Error('Ticket device fingerprint mismatch');
  }

  // Check Room ID
  if (options.expectedRoomId && ticket.rid && ticket.rid !== options.expectedRoomId) {
    throw new Error('Ticket room ID mismatch');
  }

  return ticket;
}

/**
 * Express / Connect compatible middleware.
 */
function queueguardMiddleware(options = {}) {
  const {
    secretKey,
    roomId = '',
    bindDevice = true,
    redirectUrl = '',
    ticketCookie = 'queueguard_ticket',
    sessionCookie = 'queueguard_session',
    ticketHeader = 'x-queueguard-ticket',
  } = options;

  if (!secretKey) {
    throw new Error('[QueueGuard] secretKey option is required in middleware configuration.');
  }

  return function (req, res, next) {
    const cookies = req.cookies || parseCookies(req.headers.cookie || '');
    const token = cookies[ticketCookie] || req.headers[ticketHeader];

    if (!token) {
      return rejectRequest(req, res, redirectUrl);
    }

    let devHash = '';
    if (bindDevice) {
      const clientIp = req.headers['x-forwarded-for']?.split(',')[0].trim() || req.socket.remoteAddress || '';
      const userAgent = req.headers['user-agent'] || '';
      devHash = computeDeviceFingerprint(userAgent, clientIp);
    }

    try {
      const ticket = verifyTicket(token, secretKey, {
        expectedDeviceHash: devHash,
        expectedRoomId: roomId,
      });

      // Verify matching session ID if cookie present
      const sessionId = cookies[sessionCookie];
      if (sessionId && ticket.sid !== sessionId) {
        return rejectRequest(req, res, redirectUrl);
      }

      req.queueguard = { ticket, sessionId: ticket.sid, roomId: ticket.rid };
      next();
    } catch (err) {
      return rejectRequest(req, res, redirectUrl);
    }
  };
}

function rejectRequest(req, res, redirectUrl) {
  if (redirectUrl) {
    return res.redirect(redirectUrl);
  }

  const accept = req.headers.accept || '';
  if (accept.includes('application/json')) {
    return res.status(401).json({
      error: 'unauthorized',
      message: 'Valid QueueGuard admission ticket required to access this resource',
    });
  }

  res.status(401).send('Access Denied: Valid QueueGuard admission ticket required');
}

module.exports = {
  queueguardMiddleware,
  verifyTicket,
  computeDeviceFingerprint,
};
