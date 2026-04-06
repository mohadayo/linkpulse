import express, { Request, Response, NextFunction } from 'express';
import axios, { AxiosError } from 'axios';

const GATEWAY_PORT = parseInt(process.env.GATEWAY_PORT || '8003', 10);
const SHORTENER_URL = process.env.SHORTENER_URL || 'http://shortener:8001';
const ANALYTICS_URL = process.env.ANALYTICS_URL || 'http://analytics:8002';

const app = express();
app.use(express.json());

function log(message: string): void {
  console.log(`[${new Date().toISOString()}] ${message}`);
}

// Health check
app.get('/health', (_req: Request, res: Response) => {
  res.json({ status: 'ok', service: 'gateway' });
});

// Proxy: POST /api/shorten -> shortener service
app.post('/api/shorten', async (req: Request, res: Response) => {
  try {
    log(`Proxying POST /api/shorten to ${SHORTENER_URL}/shorten`);
    const response = await axios.post(`${SHORTENER_URL}/shorten`, req.body, {
      headers: { 'Content-Type': 'application/json' },
      timeout: 10000,
    });
    res.status(response.status).json(response.data);
  } catch (err) {
    handleUpstreamError(err, 'shortener', res);
  }
});

// Proxy: GET /api/stats -> analytics service
app.get('/api/stats', async (_req: Request, res: Response) => {
  try {
    log(`Proxying GET /api/stats to ${ANALYTICS_URL}/stats`);
    const response = await axios.get(`${ANALYTICS_URL}/stats`, {
      timeout: 10000,
    });
    res.status(response.status).json(response.data);
  } catch (err) {
    handleUpstreamError(err, 'analytics', res);
  }
});

// Proxy: GET /api/stats/:code -> analytics service
app.get('/api/stats/:code', async (req: Request, res: Response) => {
  const { code } = req.params;
  try {
    log(`Proxying GET /api/stats/${code} to ${ANALYTICS_URL}/stats/${code}`);
    const response = await axios.get(`${ANALYTICS_URL}/stats/${code}`, {
      timeout: 10000,
    });
    res.status(response.status).json(response.data);
  } catch (err) {
    handleUpstreamError(err, 'analytics', res);
  }
});

// GET /api/services - check health of all upstream services
app.get('/api/services', async (_req: Request, res: Response) => {
  log('Checking status of all upstream services');

  const checkHealth = async (name: string, url: string): Promise<{ name: string; url: string; status: string }> => {
    try {
      const response = await axios.get(`${url}/health`, { timeout: 5000 });
      return { name, url, status: response.data?.status || 'ok' };
    } catch {
      return { name, url, status: 'unreachable' };
    }
  };

  const results = await Promise.all([
    checkHealth('shortener', SHORTENER_URL),
    checkHealth('analytics', ANALYTICS_URL),
  ]);

  res.json({ services: results });
});

function handleUpstreamError(err: unknown, serviceName: string, res: Response): void {
  if (err instanceof AxiosError) {
    if (err.response) {
      // Upstream returned an error status
      log(`Upstream ${serviceName} returned ${err.response.status}`);
      res.status(err.response.status).json(err.response.data);
    } else {
      // No response — connection error
      log(`Upstream ${serviceName} is unreachable: ${err.message}`);
      res.status(502).json({
        error: `Service '${serviceName}' is unavailable`,
        message: err.message,
      });
    }
  } else {
    log(`Unexpected error proxying to ${serviceName}: ${err}`);
    res.status(502).json({
      error: `Service '${serviceName}' is unavailable`,
      message: 'Unexpected error',
    });
  }
}

// Only start listening when this file is run directly (not imported in tests)
if (require.main === module) {
  app.listen(GATEWAY_PORT, () => {
    log(`LinkPulse Gateway listening on port ${GATEWAY_PORT}`);
    log(`Shortener URL: ${SHORTENER_URL}`);
    log(`Analytics URL: ${ANALYTICS_URL}`);
  });
}

export default app;
