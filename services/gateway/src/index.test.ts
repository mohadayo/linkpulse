import request from 'supertest';
import axios from 'axios';
import app from './index';

jest.mock('axios');
const mockedAxios = axios as jest.Mocked<typeof axios>;

import { AxiosError } from 'axios';

function createAxiosConnectionError(message: string): AxiosError {
  const err = new AxiosError(message, 'ECONNREFUSED');
  return err;
}

function createAxiosResponseError(status: number, data: any): AxiosError {
  const err = new AxiosError('Request failed', 'ERR_BAD_RESPONSE');
  (err as any).response = { status, data, headers: {}, config: {} as any, statusText: '' };
  return err;
}

describe('Gateway API', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  // --- Health check ---
  describe('GET /health', () => {
    it('returns status ok', async () => {
      const res = await request(app).get('/health');
      expect(res.status).toBe(200);
      expect(res.body).toEqual({ status: 'ok', service: 'gateway' });
    });
  });

  // --- POST /api/shorten ---
  describe('POST /api/shorten', () => {
    it('proxies request to shortener service', async () => {
      const payload = { url: 'https://example.com' };
      const upstreamResponse = { short_url: 'http://localhost/abc123', code: 'abc123' };

      mockedAxios.post.mockResolvedValueOnce({
        status: 200,
        data: upstreamResponse,
        headers: {},
        config: {} as any,
        statusText: 'OK',
      });

      const res = await request(app).post('/api/shorten').send(payload);
      expect(res.status).toBe(200);
      expect(res.body).toEqual(upstreamResponse);
      expect(mockedAxios.post).toHaveBeenCalledWith(
        'http://shortener:8001/shorten',
        payload,
        expect.objectContaining({ headers: { 'Content-Type': 'application/json' } }),
      );
    });

    it('returns 502 when shortener is down', async () => {
      mockedAxios.post.mockRejectedValueOnce(createAxiosConnectionError('connect ECONNREFUSED'));

      const res = await request(app).post('/api/shorten').send({ url: 'https://example.com' });
      expect(res.status).toBe(502);
      expect(res.body.error).toContain('shortener');
    });

    it('forwards upstream error status', async () => {
      mockedAxios.post.mockRejectedValueOnce(
        createAxiosResponseError(422, { error: 'Invalid URL' }),
      );

      const res = await request(app).post('/api/shorten').send({ url: 'bad' });
      expect(res.status).toBe(422);
      expect(res.body).toEqual({ error: 'Invalid URL' });
    });
  });

  // --- GET /api/stats ---
  describe('GET /api/stats', () => {
    it('proxies request to analytics service', async () => {
      const upstreamData = { total_links: 42, total_clicks: 1000 };
      mockedAxios.get.mockResolvedValueOnce({
        status: 200,
        data: upstreamData,
        headers: {},
        config: {} as any,
        statusText: 'OK',
      });

      const res = await request(app).get('/api/stats');
      expect(res.status).toBe(200);
      expect(res.body).toEqual(upstreamData);
      expect(mockedAxios.get).toHaveBeenCalledWith(
        'http://analytics:8002/stats',
        expect.objectContaining({ timeout: 10000 }),
      );
    });

    it('returns 502 when analytics is down', async () => {
      mockedAxios.get.mockRejectedValueOnce(createAxiosConnectionError('connect ECONNREFUSED'));

      const res = await request(app).get('/api/stats');
      expect(res.status).toBe(502);
      expect(res.body.error).toContain('analytics');
    });
  });

  // --- GET /api/stats/:code ---
  describe('GET /api/stats/:code', () => {
    it('proxies request with code param to analytics service', async () => {
      const upstreamData = { code: 'abc123', clicks: 55 };
      mockedAxios.get.mockResolvedValueOnce({
        status: 200,
        data: upstreamData,
        headers: {},
        config: {} as any,
        statusText: 'OK',
      });

      const res = await request(app).get('/api/stats/abc123');
      expect(res.status).toBe(200);
      expect(res.body).toEqual(upstreamData);
      expect(mockedAxios.get).toHaveBeenCalledWith(
        'http://analytics:8002/stats/abc123',
        expect.objectContaining({ timeout: 10000 }),
      );
    });

    it('returns 502 when analytics is down', async () => {
      mockedAxios.get.mockRejectedValueOnce(createAxiosConnectionError('connect ECONNREFUSED'));

      const res = await request(app).get('/api/stats/abc123');
      expect(res.status).toBe(502);
      expect(res.body.error).toContain('analytics');
    });

    it('forwards upstream 404', async () => {
      mockedAxios.get.mockRejectedValueOnce(
        createAxiosResponseError(404, { error: 'Code not found' }),
      );

      const res = await request(app).get('/api/stats/nonexistent');
      expect(res.status).toBe(404);
      expect(res.body).toEqual({ error: 'Code not found' });
    });
  });

  // --- GET /api/services ---
  describe('GET /api/services', () => {
    it('returns status of all services when healthy', async () => {
      mockedAxios.get.mockResolvedValueOnce({
        status: 200,
        data: { status: 'ok' },
        headers: {},
        config: {} as any,
        statusText: 'OK',
      });
      mockedAxios.get.mockResolvedValueOnce({
        status: 200,
        data: { status: 'ok' },
        headers: {},
        config: {} as any,
        statusText: 'OK',
      });

      const res = await request(app).get('/api/services');
      expect(res.status).toBe(200);
      expect(res.body.services).toHaveLength(2);
      expect(res.body.services[0].status).toBe('ok');
      expect(res.body.services[1].status).toBe('ok');
    });

    it('marks unreachable services', async () => {
      mockedAxios.get.mockResolvedValueOnce({
        status: 200,
        data: { status: 'ok' },
        headers: {},
        config: {} as any,
        statusText: 'OK',
      });
      mockedAxios.get.mockRejectedValueOnce(createAxiosConnectionError('connect ECONNREFUSED'));

      const res = await request(app).get('/api/services');
      expect(res.status).toBe(200);
      expect(res.body.services).toHaveLength(2);
      expect(res.body.services[0].status).toBe('ok');
      expect(res.body.services[1].status).toBe('unreachable');
    });
  });

  // --- Security headers (helmet) ---
  describe('Security headers', () => {
    it('sets X-Content-Type-Options header', async () => {
      const res = await request(app).get('/health');
      expect(res.headers['x-content-type-options']).toBe('nosniff');
    });

    it('sets X-Frame-Options header', async () => {
      const res = await request(app).get('/health');
      expect(res.headers['x-frame-options']).toBeDefined();
    });

    it('sets X-DNS-Prefetch-Control header', async () => {
      const res = await request(app).get('/health');
      expect(res.headers['x-dns-prefetch-control']).toBeDefined();
    });
  });
});
