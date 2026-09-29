import axios from 'axios';

// Create an axios instance, we can configure base URL or interceptors here later if needed
export const api = axios.create({
  baseURL: import.meta.env.VITE_API_BASE_URL || '',
});

// Generic fetcher for SWR
export const fetcher = (url: string) => api.get(url).then(res => res.data);
