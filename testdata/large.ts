// ============================================================================
// DataPipeline Service - Main Module
// ============================================================================

import { EventEmitter } from "events";
import { Writable } from "stream";
import { URL } from "url";
import { createHash, randomBytes, createCipheriv, createDecipheriv } from "crypto";
import { readFile, writeFile, mkdir, stat, readdir, unlink } from "fs/promises";
import { join, resolve, basename, extname, dirname } from "path";

// ============================================================================
// Enums
// ============================================================================

export enum LogLevel {
  TRACE = 0,
  DEBUG = 1,
  INFO = 2,
  WARN = 3,
  ERROR = 4,
  FATAL = 5,
}

export enum PipelineStage {
  IDLE = "idle",
  INGESTING = "ingesting",
  VALIDATING = "validating",
  TRANSFORMING = "transforming",
  ENRICHING = "enriching",
  LOADING = "loading",
  COMPLETE = "complete",
  FAILED = "failed",
}

export enum HttpMethod {
  GET = "GET",
  POST = "POST",
  PUT = "PUT",
  PATCH = "PATCH",
  DELETE = "DELETE",
  HEAD = "HEAD",
  OPTIONS = "OPTIONS",
}

// ============================================================================
// Type Aliases
// ============================================================================

export type RecordId = string;

export type TransformFn<TIn, TOut> = (input: TIn, ctx: TransformContext) => Promise<TOut>;

export type ValidatorFn<T> = (value: T, field: string) => ValidationResult;

export type MiddlewareFn = (
  req: ApiRequest,
  res: ApiResponse,
  next: () => Promise<void>
) => Promise<void>;

export type EventHandler<T = unknown> = (event: PipelineEvent<T>) => void | Promise<void>;

// ============================================================================
// Interfaces
// ============================================================================

export interface PipelineConfig {
  name: string;
  version: string;
  maxConcurrency: number;
  retryAttempts: number;
  retryDelayMs: number;
  batchSize: number;
  timeoutMs: number;
  enableMetrics: boolean;
  enableTracing: boolean;
  logLevel: LogLevel;
  outputDirectory: string;
  tempDirectory: string;
}

export interface DataRecord {
  id: RecordId;
  source: string;
  timestamp: number;
  payload: Record<string, unknown>;
  metadata: RecordMetadata;
  schemaVersion: string;
}

export interface RecordMetadata {
  createdAt: Date;
  updatedAt: Date;
  processedAt?: Date;
  checksum: string;
  sizeBytes: number;
  tags: string[];
  priority: number;
  ttlSeconds?: number;
}

export interface ValidationResult {
  valid: boolean;
  errors: ValidationError[];
  warnings: string[];
  fieldPath: string;
}

export interface ValidationError {
  code: string;
  message: string;
  fieldPath: string;
  expectedType?: string;
  actualValue?: unknown;
  severity: "error" | "warning" | "info";
}

export interface TransformContext {
  pipelineId: string;
  stageIndex: number;
  stageName: string;
  attemptNumber: number;
  startTime: number;
  metadata: Map<string, unknown>;
}

export interface ApiRequest {
  method: HttpMethod;
  path: string;
  headers: Record<string, string>;
  query: Record<string, string | string[]>;
  body: unknown;
  params: Record<string, string>;
  timestamp: number;
  requestId: string;
}

export interface ApiResponse {
  statusCode: number;
  headers: Record<string, string>;
  body: unknown;
  sentAt?: number;
}

export interface PipelineEvent<T = unknown> {
  type: string;
  source: string;
  timestamp: number;
  correlationId: string;
  data: T;
}

export interface MetricsSnapshot {
  recordsProcessed: number;
  recordsFailed: number;
  recordsSkipped: number;
  averageLatencyMs: number;
  p95LatencyMs: number;
  p99LatencyMs: number;
  throughputPerSecond: number;
  activeWorkers: number;
  queueDepth: number;
  errorRate: number;
  uptime: number;
}

interface FieldSchema {
  type: string;
  required: boolean;
  minLength?: number;
  maxLength?: number;
  pattern?: string;
}

interface RouteEntry {
  method: HttpMethod;
  path: string;
  handler: RouteHandler;
}

type RouteHandler = (req: ApiRequest, res: ApiResponse) => Promise<ApiResponse>;

interface CacheEntry<T> {
  value: T;
  createdAt: number;
  expiresAt: number;
  accessCount: number;
  lastAccessedAt: number;
}

// ============================================================================
// Utility Functions (standalone, named, exported, various signatures)
// ============================================================================

export function generateRecordId(): RecordId {
  const ts = Date.now().toString(36);
  const rand = randomBytes(8).toString("hex");
  return `rec_${ts}_${rand}`;
}

export function computeChecksum(data: string | Buffer): string {
  const hash = createHash("sha256");
  hash.update(typeof data === "string" ? Buffer.from(data, "utf-8") : data);
  return hash.digest("hex");
}

function clampValue(value: number, min: number, max: number): number {
  if (value < min) return min;
  if (value > max) return max;
  return value;
}

export function formatBytes(bytes: number): string {
  const units = ["B", "KB", "MB", "GB", "TB"];
  let idx = 0;
  let remaining = Math.abs(bytes);
  while (remaining >= 1024 && idx < units.length - 1) {
    remaining /= 1024;
    idx++;
  }
  const sign = bytes < 0 ? "-" : "";
  return `${sign}${remaining.toFixed(idx === 0 ? 0 : 2)} ${units[idx]}`;
}

export function parseQueryString(qs: string): Record<string, string | string[]> {
  const result: Record<string, string | string[]> = {};
  if (!qs) return result;
  const cleaned = qs.startsWith("?") ? qs.slice(1) : qs;
  for (const pair of cleaned.split("&")) {
    const eq = pair.indexOf("=");
    if (eq === -1) { result[decodeURIComponent(pair)] = ""; continue; }
    const key = decodeURIComponent(pair.slice(0, eq));
    const val = decodeURIComponent(pair.slice(eq + 1));
    const existing = result[key];
    if (existing === undefined) result[key] = val;
    else if (Array.isArray(existing)) existing.push(val);
    else result[key] = [existing, val];
  }
  return result;
}

function sanitizeFieldName(name: string): string {
  return name.replace(/[^a-zA-Z0-9_]/g, "_").replace(/_{2,}/g, "_")
    .replace(/^_+|_+$/g, "").toLowerCase();
}

export async function sleepMs(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms));
}

export async function retryWithBackoff<T>(
  operation: () => Promise<T>,
  maxAttempts: number,
  baseDelayMs: number
): Promise<T> {
  let lastError: Error | undefined;
  for (let attempt = 1; attempt <= maxAttempts; attempt++) {
    try {
      return await operation();
    } catch (error) {
      lastError = error instanceof Error ? error : new Error(String(error));
      if (attempt < maxAttempts) {
        const jitter = Math.random() * 0.3 + 0.85;
        await sleepMs(baseDelayMs * Math.pow(2, attempt - 1) * jitter);
      }
    }
  }
  throw lastError ?? new Error("All retry attempts exhausted");
}

function deepClone<T>(obj: T): T {
  if (obj === null || typeof obj !== "object") return obj;
  if (obj instanceof Date) return new Date(obj.getTime()) as unknown as T;
  if (Array.isArray(obj)) return obj.map((item) => deepClone(item)) as unknown as T;
  const cloned: Record<string, unknown> = {};
  for (const key of Object.keys(obj as Record<string, unknown>)) {
    cloned[key] = deepClone((obj as Record<string, unknown>)[key]);
  }
  return cloned as T;
}

export function mergeConfigs<T extends Record<string, unknown>>(base: T, overrides: Partial<T>): T {
  const result = deepClone(base);
  for (const key of Object.keys(overrides) as Array<keyof T>) {
    const ov = overrides[key];
    if (ov === undefined) continue;
    const bv = result[key];
    if (typeof bv === "object" && bv !== null && !Array.isArray(bv) &&
        typeof ov === "object" && ov !== null && !Array.isArray(ov)) {
      (result as Record<string, unknown>)[key as string] = mergeConfigs(
        bv as Record<string, unknown>, ov as Record<string, unknown>
      );
    } else {
      (result as Record<string, unknown>)[key as string] = deepClone(ov);
    }
  }
  return result;
}

function truncateString(input: string, maxLength: number): string {
  if (input.length <= maxLength) return input;
  if (maxLength <= 3) return "...".slice(0, maxLength);
  return input.slice(0, maxLength - 3) + "...";
}

export function flattenObject(obj: Record<string, unknown>, prefix = ""): Record<string, unknown> {
  const result: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(obj)) {
    const fullKey = prefix ? `${prefix}.${key}` : key;
    if (typeof value === "object" && value !== null && !Array.isArray(value) && !(value instanceof Date)) {
      Object.assign(result, flattenObject(value as Record<string, unknown>, fullKey));
    } else {
      result[fullKey] = value;
    }
  }
  return result;
}

export async function ensureDirectory(dirPath: string): Promise<void> {
  try {
    const stats = await stat(dirPath);
    if (!stats.isDirectory()) throw new Error(`Not a directory: ${dirPath}`);
  } catch (error: unknown) {
    if (error instanceof Error && (error as NodeJS.ErrnoException).code === "ENOENT") {
      await mkdir(dirPath, { recursive: true });
    } else {
      throw error;
    }
  }
}

export function buildUrlWithParams(baseUrl: string, params: Record<string, string | number | boolean>): string {
  const url = new URL(baseUrl);
  for (const [key, value] of Object.entries(params)) {
    url.searchParams.set(key, String(value));
  }
  return url.toString();
}

function calculatePercentile(sorted: number[], pct: number): number {
  if (sorted.length === 0) return 0;
  const idx = (clampValue(pct, 0, 100) / 100) * (sorted.length - 1);
  const lo = Math.floor(idx);
  const hi = Math.ceil(idx);
  if (lo === hi) return sorted[lo];
  const w = idx - lo;
  return sorted[lo] * (1 - w) + sorted[hi] * w;
}

function isNonEmptyString(value: unknown): value is string {
  return typeof value === "string" && value.trim().length > 0;
}

export function parseCsvLine(line: string, delimiter = ","): string[] {
  const fields: string[] = [];
  let current = "";
  let inQuotes = false;
  for (let i = 0; i < line.length; i++) {
    const ch = line[i];
    const next = i + 1 < line.length ? line[i + 1] : null;
    if (inQuotes) {
      if (ch === '"' && next === '"') { current += '"'; i++; }
      else if (ch === '"') inQuotes = false;
      else current += ch;
    } else {
      if (ch === '"') inQuotes = true;
      else if (ch === delimiter) { fields.push(current); current = ""; }
      else current += ch;
    }
  }
  fields.push(current);
  return fields;
}

export function parseCsvContent(
  content: string,
  options: { delimiter?: string; hasHeader?: boolean } = {}
): { headers: string[]; rows: Record<string, string>[] } {
  const delim = options.delimiter ?? ",";
  const hasHeader = options.hasHeader ?? true;
  const lines = content.split("\n").filter((l) => l.trim().length > 0);
  if (lines.length === 0) return { headers: [], rows: [] };
  const headers = hasHeader
    ? parseCsvLine(lines[0], delim).map((h) => h.trim())
    : parseCsvLine(lines[0], delim).map((_, i) => `column_${i}`);
  const startIdx = hasHeader ? 1 : 0;
  const rows: Record<string, string>[] = [];
  for (let i = startIdx; i < lines.length; i++) {
    const fields = parseCsvLine(lines[i], delim);
    const row: Record<string, string> = {};
    for (let j = 0; j < headers.length; j++) {
      row[headers[j]] = j < fields.length ? fields[j].trim() : "";
    }
    rows.push(row);
  }
  return { headers, rows };
}

export function normalizeRecord(record: DataRecord): DataRecord {
  const cloned = deepClone(record);
  cloned.source = cloned.source.trim().toLowerCase();
  if (cloned.payload && typeof cloned.payload === "object") {
    const normalized: Record<string, unknown> = {};
    for (const [key, value] of Object.entries(cloned.payload)) {
      normalized[sanitizeFieldName(key)] = value;
    }
    cloned.payload = normalized;
  }
  return cloned;
}

export function filterRecordsBySource(records: DataRecord[], sources: string[]): DataRecord[] {
  const set = new Set(sources.map((s) => s.toLowerCase()));
  return records.filter((r) => set.has(r.source.toLowerCase()));
}

export function sortRecordsByTimestamp(records: DataRecord[], asc = true): DataRecord[] {
  return [...records].sort((a, b) => asc ? a.timestamp - b.timestamp : b.timestamp - a.timestamp);
}

export function deduplicateRecords(records: DataRecord[]): DataRecord[] {
  const seen = new Map<string, DataRecord>();
  for (const record of records) {
    const existing = seen.get(record.metadata.checksum);
    if (!existing || record.timestamp > existing.timestamp) {
      seen.set(record.metadata.checksum, record);
    }
  }
  return Array.from(seen.values());
}

// ============================================================================
// Arrow Functions
// ============================================================================

export const createDefaultConfig = (): PipelineConfig => ({
  name: "default-pipeline",
  version: "1.0.0",
  maxConcurrency: 4,
  retryAttempts: 3,
  retryDelayMs: 1000,
  batchSize: 100,
  timeoutMs: 30000,
  enableMetrics: true,
  enableTracing: false,
  logLevel: LogLevel.INFO,
  outputDirectory: "./output",
  tempDirectory: "./tmp",
});

export const isValidRecordId = (id: unknown): id is RecordId => {
  return typeof id === "string" && /^rec_[a-z0-9]+_[a-f0-9]{16}$/.test(id);
};

const normalizeTimestamp = (input: string | number | Date): number => {
  if (input instanceof Date) return input.getTime();
  if (typeof input === "number") return input < 1e12 ? input * 1000 : input;
  const parsed = Date.parse(input);
  if (isNaN(parsed)) throw new Error(`Invalid timestamp: ${input}`);
  return parsed;
};

export const mapRecordToSummary = (
  record: DataRecord
): { id: string; source: string; size: string; age: string } => {
  const ageMin = Math.floor((Date.now() - record.timestamp) / 60000);
  const age = ageMin < 60 ? `${ageMin}m ago` : ageMin < 1440 ? `${Math.floor(ageMin / 60)}h ago` : `${Math.floor(ageMin / 1440)}d ago`;
  return { id: record.id, source: record.source, size: formatBytes(record.metadata.sizeBytes), age };
};

const buildErrorResponse = (code: number, message: string, details?: Record<string, unknown>): ApiResponse => ({
  statusCode: code,
  headers: { "Content-Type": "application/json", "X-Error": "true" },
  body: { error: { code, message, details: details ?? null, timestamp: Date.now() } },
});

export const composeTransforms = <T>(...fns: Array<(input: T) => T>): ((input: T) => T) => {
  return (input: T): T => {
    let result = input;
    for (const fn of fns) result = fn(result);
    return result;
  };
};

const debounce = <TArgs extends unknown[]>(fn: (...args: TArgs) => void, delayMs: number): ((...args: TArgs) => void) => {
  let timeoutId: ReturnType<typeof setTimeout> | null = null;
  return (...args: TArgs): void => {
    if (timeoutId !== null) clearTimeout(timeoutId);
    timeoutId = setTimeout(() => { fn(...args); timeoutId = null; }, delayMs);
  };
};

export const asyncFilter = async <T>(items: T[], predicate: (item: T) => Promise<boolean>): Promise<T[]> => {
  const results = await Promise.all(items.map(async (item) => ({ item, keep: await predicate(item) })));
  return results.filter((r) => r.keep).map((r) => r.item);
};

const chunkArray = <T>(array: T[], size: number): T[][] => {
  const chunks: T[][] = [];
  for (let i = 0; i < array.length; i += size) chunks.push(array.slice(i, i + size));
  return chunks;
};

export const createTimestampedFilename = (prefix: string, extension: string): string => {
  const d = new Date();
  const pad = (n: number) => String(n).padStart(2, "0");
  const dateStr = `${d.getFullYear()}${pad(d.getMonth() + 1)}${pad(d.getDate())}_${pad(d.getHours())}${pad(d.getMinutes())}${pad(d.getSeconds())}`;
  const ext = extension.startsWith(".") ? extension : `.${extension}`;
  return `${prefix}_${dateStr}${ext}`;
};

// ============================================================================
// Class: Logger
// ============================================================================

export class Logger {
  private static instance: Logger | null = null;
  private level: LogLevel;
  private readonly context: string;
  private readonly outputStream: Writable;
  private messageCount: number;
  private readonly startTime: number;

  constructor(context: string, level: LogLevel = LogLevel.INFO) {
    this.context = context;
    this.level = level;
    this.outputStream = process.stdout;
    this.messageCount = 0;
    this.startTime = Date.now();
  }

  public static getInstance(): Logger {
    if (!Logger.instance) Logger.instance = new Logger("global");
    return Logger.instance;
  }

  public static resetInstance(): void { Logger.instance = null; }

  public setLevel(level: LogLevel): void { this.level = level; }
  public getLevel(): LogLevel { return this.level; }
  get elapsedMs(): number { return Date.now() - this.startTime; }
  get totalMessages(): number { return this.messageCount; }

  public trace(msg: string, data?: Record<string, unknown>): void { this.log(LogLevel.TRACE, msg, data); }
  public debug(msg: string, data?: Record<string, unknown>): void { this.log(LogLevel.DEBUG, msg, data); }
  public info(msg: string, data?: Record<string, unknown>): void { this.log(LogLevel.INFO, msg, data); }
  public warn(msg: string, data?: Record<string, unknown>): void { this.log(LogLevel.WARN, msg, data); }

  public error(msg: string, err?: Error, data?: Record<string, unknown>): void {
    this.log(LogLevel.ERROR, msg, { ...data, errorName: err?.name, errorMessage: err?.message, stack: err?.stack });
  }

  public fatal(msg: string, err?: Error): void {
    this.log(LogLevel.FATAL, msg, { errorName: err?.name, errorMessage: err?.message, stack: err?.stack });
  }

  private log(level: LogLevel, message: string, data?: Record<string, unknown>): void {
    if (level < this.level) return;
    this.messageCount++;
    const entry = { timestamp: new Date().toISOString(), level: LogLevel[level], context: this.context, message, ...(data ? { data } : {}) };
    this.outputStream.write(JSON.stringify(entry) + "\n");
  }

  public child(sub: string): Logger { return new Logger(`${this.context}.${sub}`, this.level); }
}

// ============================================================================
// Class: SchemaValidator
// ============================================================================

export class SchemaValidator {
  private readonly schemas: Map<string, Record<string, FieldSchema>>;
  private readonly strictMode: boolean;
  private validationCount: number;
  private errorCount: number;
  private readonly logger: Logger;

  constructor(strict = true) {
    this.schemas = new Map();
    this.strictMode = strict;
    this.validationCount = 0;
    this.errorCount = 0;
    this.logger = new Logger("SchemaValidator");
  }

  public registerSchema(name: string, fields: Record<string, FieldSchema>): void {
    if (this.schemas.has(name)) this.logger.warn(`Overwriting schema: ${name}`);
    this.schemas.set(name, fields);
    this.logger.info(`Registered schema: ${name}`, { fieldCount: Object.keys(fields).length });
  }

  public getSchemaNames(): string[] { return Array.from(this.schemas.keys()).sort(); }

  get stats(): { validations: number; errors: number; schemas: number } {
    return { validations: this.validationCount, errors: this.errorCount, schemas: this.schemas.size };
  }

  public validate(schemaName: string, data: Record<string, unknown>): ValidationResult {
    this.validationCount++;
    const schema = this.schemas.get(schemaName);
    if (!schema) {
      this.errorCount++;
      return { valid: false, errors: [{ code: "SCHEMA_NOT_FOUND", message: `Schema '${schemaName}' not registered`, fieldPath: "", severity: "error" }], warnings: [], fieldPath: "" };
    }
    const errors: ValidationError[] = [];
    const warnings: string[] = [];
    for (const [name, fs] of Object.entries(schema)) {
      errors.push(...this.validateField(name, data[name], fs));
    }
    if (this.strictMode) {
      for (const k of Object.keys(data)) {
        if (!(k in schema)) warnings.push(`Unknown field '${k}'`);
      }
    }
    if (errors.length > 0) this.errorCount++;
    return { valid: errors.length === 0, errors, warnings, fieldPath: "" };
  }

  private validateField(name: string, value: unknown, schema: FieldSchema): ValidationError[] {
    const errs: ValidationError[] = [];
    if (value === undefined || value === null) {
      if (schema.required) errs.push({ code: "REQUIRED", message: `'${name}' is required`, fieldPath: name, expectedType: schema.type, severity: "error" });
      return errs;
    }
    const actual = Array.isArray(value) ? "array" : typeof value;
    if (actual !== schema.type && schema.type !== "any") {
      errs.push({ code: "TYPE_MISMATCH", message: `'${name}' expected '${schema.type}' got '${actual}'`, fieldPath: name, expectedType: schema.type, actualValue: value, severity: "error" });
    }
    if (schema.minLength !== undefined && typeof value === "string" && value.length < schema.minLength) {
      errs.push({ code: "MIN_LENGTH", message: `'${name}' too short (min ${schema.minLength})`, fieldPath: name, severity: "error" });
    }
    if (schema.maxLength !== undefined && typeof value === "string" && value.length > schema.maxLength) {
      errs.push({ code: "MAX_LENGTH", message: `'${name}' too long (max ${schema.maxLength})`, fieldPath: name, severity: "error" });
    }
    if (schema.pattern !== undefined && typeof value === "string" && !new RegExp(schema.pattern).test(value)) {
      errs.push({ code: "PATTERN", message: `'${name}' does not match /${schema.pattern}/`, fieldPath: name, severity: "error" });
    }
    return errs;
  }

  public removeSchema(name: string): boolean {
    const existed = this.schemas.delete(name);
    if (existed) this.logger.info(`Removed schema: ${name}`);
    return existed;
  }

  public cloneSchema(src: string, dst: string): boolean {
    const source = this.schemas.get(src);
    if (!source) return false;
    this.schemas.set(dst, deepClone(source));
    return true;
  }
}

// ============================================================================
// Class: DataPipeline
// ============================================================================

export class DataPipeline {
  private readonly config: PipelineConfig;
  private readonly logger: Logger;
  private readonly validator: SchemaValidator;
  private readonly emitter: EventEmitter;
  private currentStage: PipelineStage;
  private readonly processed: Map<RecordId, DataRecord>;
  private readonly failed: Map<RecordId, { record: DataRecord; error: string }>;
  private readonly latencies: number[];
  private isRunning: boolean;
  private startTimestamp: number | null;

  constructor(config: Partial<PipelineConfig> = {}) {
    this.config = mergeConfigs(createDefaultConfig(), config);
    this.logger = new Logger(`Pipeline:${this.config.name}`, this.config.logLevel);
    this.validator = new SchemaValidator(true);
    this.emitter = new EventEmitter();
    this.currentStage = PipelineStage.IDLE;
    this.processed = new Map();
    this.failed = new Map();
    this.latencies = [];
    this.isRunning = false;
    this.startTimestamp = null;
    this.registerDefaultSchemas();
  }

  get stage(): PipelineStage { return this.currentStage; }

  get metrics(): MetricsSnapshot {
    const sorted = [...this.latencies].sort((a, b) => a - b);
    const total = this.processed.size + this.failed.size;
    const avg = sorted.length > 0 ? sorted.reduce((s, v) => s + v, 0) / sorted.length : 0;
    const uptimeMs = this.startTimestamp ? Date.now() - this.startTimestamp : 0;
    const tput = uptimeMs > 0 ? (this.processed.size / uptimeMs) * 1000 : 0;
    return {
      recordsProcessed: this.processed.size,
      recordsFailed: this.failed.size,
      recordsSkipped: 0,
      averageLatencyMs: Math.round(avg * 100) / 100,
      p95LatencyMs: calculatePercentile(sorted, 95),
      p99LatencyMs: calculatePercentile(sorted, 99),
      throughputPerSecond: Math.round(tput * 100) / 100,
      activeWorkers: this.isRunning ? this.config.maxConcurrency : 0,
      queueDepth: 0,
      errorRate: total > 0 ? Math.round((this.failed.size / total) * 10000) / 100 : 0,
      uptime: uptimeMs,
    };
  }

  private registerDefaultSchemas(): void {
    this.validator.registerSchema("dataRecord", {
      id: { type: "string", required: true, pattern: "^rec_" },
      source: { type: "string", required: true, minLength: 1 },
      timestamp: { type: "number", required: true },
      payload: { type: "object", required: true },
      schemaVersion: { type: "string", required: true },
    });
  }

  public on<T>(type: string, handler: EventHandler<T>): void { this.emitter.on(type, handler); }
  public off<T>(type: string, handler: EventHandler<T>): void { this.emitter.off(type, handler); }

  private emit<T>(type: string, data: T): void {
    const event: PipelineEvent<T> = { type, source: this.config.name, timestamp: Date.now(), correlationId: generateRecordId(), data };
    this.emitter.emit(type, event);
  }

  public async start(): Promise<void> {
    if (this.isRunning) { this.logger.warn("Already running"); return; }
    this.isRunning = true;
    this.startTimestamp = Date.now();
    this.currentStage = PipelineStage.IDLE;
    this.logger.info("Pipeline started", { name: this.config.name });
    this.emit("pipeline:started", { name: this.config.name });
    await ensureDirectory(this.config.outputDirectory);
    await ensureDirectory(this.config.tempDirectory);
  }

  public async stop(): Promise<void> {
    if (!this.isRunning) return;
    this.isRunning = false;
    this.currentStage = PipelineStage.IDLE;
    const m = this.metrics;
    this.logger.info("Pipeline stopped", { processed: m.recordsProcessed, failed: m.recordsFailed });
    this.emit("pipeline:stopped", m);
  }

  public async processRecord(record: DataRecord): Promise<boolean> {
    if (!this.isRunning) throw new Error("Pipeline not running");
    const t0 = Date.now();
    try {
      this.currentStage = PipelineStage.VALIDATING;
      const vr = this.validator.validate("dataRecord", {
        id: record.id, source: record.source, timestamp: record.timestamp,
        payload: record.payload, schemaVersion: record.schemaVersion,
      });
      if (!vr.valid) throw new Error(`Validation failed: ${vr.errors.map((e) => e.message).join("; ")}`);
      this.currentStage = PipelineStage.TRANSFORMING;
      const transformed = await this.applyTransforms(record);
      this.currentStage = PipelineStage.ENRICHING;
      const enriched = await this.enrichRecord(transformed);
      this.currentStage = PipelineStage.LOADING;
      await this.persistRecord(enriched);
      enriched.metadata.processedAt = new Date();
      this.processed.set(enriched.id, enriched);
      const latency = Date.now() - t0;
      this.latencies.push(latency);
      this.emit("record:processed", { id: enriched.id, latencyMs: latency });
      this.currentStage = PipelineStage.IDLE;
      return true;
    } catch (error) {
      const msg = error instanceof Error ? error.message : String(error);
      this.failed.set(record.id, { record, error: msg });
      this.logger.error("Record failed", error as Error, { recordId: record.id });
      this.emit("record:failed", { id: record.id, error: msg });
      this.currentStage = PipelineStage.IDLE;
      return false;
    }
  }

  public async processBatch(records: DataRecord[]): Promise<{ succeeded: number; failed: number; results: Map<RecordId, boolean> }> {
    const results = new Map<RecordId, boolean>();
    let succeeded = 0;
    let failed = 0;
    for (const batch of chunkArray(records, this.config.batchSize)) {
      await Promise.all(batch.map(async (r) => {
        const ok = await this.processRecord(r);
        results.set(r.id, ok);
        if (ok) succeeded++; else failed++;
      }));
    }
    this.logger.info("Batch complete", { succeeded, failed });
    return { succeeded, failed, results };
  }

  private async applyTransforms(record: DataRecord): Promise<DataRecord> {
    const cloned = deepClone(record);
    if (typeof cloned.payload === "object" && cloned.payload !== null) {
      const flat = flattenObject(cloned.payload);
      for (const [k, v] of Object.entries(flat)) {
        if (typeof v === "string") (flat as Record<string, unknown>)[k] = v.trim();
      }
    }
    cloned.metadata.updatedAt = new Date();
    return cloned;
  }

  private async enrichRecord(record: DataRecord): Promise<DataRecord> {
    record.metadata.checksum = computeChecksum(JSON.stringify(record.payload));
    record.metadata.sizeBytes = Buffer.byteLength(JSON.stringify(record.payload), "utf-8");
    if (!record.metadata.tags.includes("processed")) record.metadata.tags.push("processed");
    return record;
  }

  protected async persistRecord(record: DataRecord): Promise<void> {
    const filename = createTimestampedFilename(record.id, "json");
    const filepath = join(this.config.outputDirectory, filename);
    await writeFile(filepath, JSON.stringify(record, null, 2), "utf-8");
    this.logger.debug(`Persisted: ${filepath}`);
  }
}

// ============================================================================
// Class: ApiRouter
// ============================================================================

export class ApiRouter {
  private readonly routes: Map<string, RouteEntry[]>;
  private readonly middlewareStack: MiddlewareFn[];
  private readonly logger: Logger;
  private requestCount: number;
  private errorCount: number;

  constructor() {
    this.routes = new Map();
    this.middlewareStack = [];
    this.logger = new Logger("ApiRouter");
    this.requestCount = 0;
    this.errorCount = 0;
  }

  get routeCount(): number {
    let c = 0;
    for (const entries of this.routes.values()) c += entries.length;
    return c;
  }

  get stats(): { requests: number; errors: number; routes: number } {
    return { requests: this.requestCount, errors: this.errorCount, routes: this.routeCount };
  }

  public use(mw: MiddlewareFn): void { this.middlewareStack.push(mw); }
  public get(path: string, handler: RouteHandler): void { this.addRoute(HttpMethod.GET, path, handler); }
  public post(path: string, handler: RouteHandler): void { this.addRoute(HttpMethod.POST, path, handler); }
  public put(path: string, handler: RouteHandler): void { this.addRoute(HttpMethod.PUT, path, handler); }
  public delete(path: string, handler: RouteHandler): void { this.addRoute(HttpMethod.DELETE, path, handler); }

  private addRoute(method: HttpMethod, path: string, handler: RouteHandler): void {
    const np = this.normalizePath(path);
    const existing = this.routes.get(np) ?? [];
    existing.push({ method, handler, path: np });
    this.routes.set(np, existing);
    this.logger.info(`Route: ${method} ${np}`);
  }

  private normalizePath(path: string): string {
    let p = path.trim();
    if (!p.startsWith("/")) p = "/" + p;
    if (p.length > 1 && p.endsWith("/")) p = p.slice(0, -1);
    return p;
  }

  public async handleRequest(request: ApiRequest): Promise<ApiResponse> {
    this.requestCount++;
    const t0 = Date.now();
    try {
      const response: ApiResponse = { statusCode: 200, headers: {}, body: null };
      await this.executeMiddleware(request, response);
      const np = this.normalizePath(request.path);
      const entries = this.routes.get(np);
      if (!entries || entries.length === 0) return buildErrorResponse(404, `Not found: ${request.path}`);
      const matched = entries.find((e) => e.method === request.method);
      if (!matched) {
        const allowed = entries.map((e) => e.method).join(", ");
        const resp = buildErrorResponse(405, `Method not allowed. Allowed: ${allowed}`);
        resp.headers["Allow"] = allowed;
        return resp;
      }
      const result = await matched.handler(request, response);
      result.sentAt = Date.now();
      this.logger.info("Handled", { method: request.method, path: request.path, status: result.statusCode, ms: Date.now() - t0 });
      return result;
    } catch (error) {
      this.errorCount++;
      this.logger.error("Request failed", error as Error, { path: request.path });
      return buildErrorResponse(500, error instanceof Error ? error.message : "Internal error");
    }
  }

  private async executeMiddleware(req: ApiRequest, res: ApiResponse): Promise<void> {
    let idx = 0;
    const stack = this.middlewareStack;
    const next = async (): Promise<void> => { if (idx < stack.length) { const mw = stack[idx]; idx++; await mw(req, res, next); } };
    await next();
  }

  public removeRoute(method: HttpMethod, path: string): boolean {
    const np = this.normalizePath(path);
    const entries = this.routes.get(np);
    if (!entries) return false;
    const filtered = entries.filter((e) => e.method !== method);
    if (filtered.length === entries.length) return false;
    if (filtered.length === 0) this.routes.delete(np);
    else this.routes.set(np, filtered);
    return true;
  }
}

// ============================================================================
// Class: CacheManager
// ============================================================================

export class CacheManager<TValue = unknown> {
  private readonly store: Map<string, CacheEntry<TValue>>;
  private readonly maxSize: number;
  private readonly defaultTtlMs: number;
  private readonly logger: Logger;
  private hits: number;
  private misses: number;
  private evictions: number;

  constructor(maxSize = 1000, defaultTtlMs = 300000) {
    this.store = new Map();
    this.maxSize = maxSize;
    this.defaultTtlMs = defaultTtlMs;
    this.logger = new Logger("CacheManager");
    this.hits = 0;
    this.misses = 0;
    this.evictions = 0;
  }

  get size(): number { return this.store.size; }

  get hitRate(): number {
    const total = this.hits + this.misses;
    return total === 0 ? 0 : Math.round((this.hits / total) * 10000) / 100;
  }

  get statistics(): { size: number; maxSize: number; hits: number; misses: number; evictions: number; hitRate: number } {
    return { size: this.store.size, maxSize: this.maxSize, hits: this.hits, misses: this.misses, evictions: this.evictions, hitRate: this.hitRate };
  }

  public set(key: string, value: TValue, ttlMs?: number): void {
    if (this.store.size >= this.maxSize && !this.store.has(key)) this.evictOldest();
    this.store.set(key, { value, createdAt: Date.now(), expiresAt: Date.now() + (ttlMs ?? this.defaultTtlMs), accessCount: 0, lastAccessedAt: Date.now() });
  }

  public get(key: string): TValue | undefined {
    const entry = this.store.get(key);
    if (!entry) { this.misses++; return undefined; }
    if (Date.now() > entry.expiresAt) { this.store.delete(key); this.misses++; return undefined; }
    entry.accessCount++;
    entry.lastAccessedAt = Date.now();
    this.hits++;
    return entry.value;
  }

  public has(key: string): boolean {
    const entry = this.store.get(key);
    if (!entry) return false;
    if (Date.now() > entry.expiresAt) { this.store.delete(key); return false; }
    return true;
  }

  public delete(key: string): boolean { return this.store.delete(key); }
  public clear(): void { const s = this.store.size; this.store.clear(); this.logger.info(`Cleared ${s} entries`); }

  private evictOldest(): void {
    let oldestKey: string | null = null;
    let oldestTime = Infinity;
    for (const [key, entry] of this.store.entries()) {
      if (entry.lastAccessedAt < oldestTime) { oldestTime = entry.lastAccessedAt; oldestKey = key; }
    }
    if (oldestKey !== null) { this.store.delete(oldestKey); this.evictions++; }
  }

  public async getOrSet(key: string, factory: () => Promise<TValue>, ttlMs?: number): Promise<TValue> {
    const existing = this.get(key);
    if (existing !== undefined) return existing;
    const value = await factory();
    this.set(key, value, ttlMs);
    return value;
  }

  public prune(): number {
    const now = Date.now();
    let pruned = 0;
    for (const [key, entry] of this.store.entries()) {
      if (now > entry.expiresAt) { this.store.delete(key); pruned++; }
    }
    if (pruned > 0) this.logger.debug(`Pruned ${pruned} entries`);
    return pruned;
  }
}

// ============================================================================
// Class: FileProcessor
// ============================================================================

export class FileProcessor {
  private readonly baseDir: string;
  private readonly logger: Logger;
  private readonly extensions: Set<string>;
  private filesProcessed: number;
  private totalBytesRead: number;
  private readonly cache: CacheManager<string>;

  constructor(baseDirectory: string) {
    this.baseDir = resolve(baseDirectory);
    this.logger = new Logger("FileProcessor");
    this.extensions = new Set([".json", ".csv", ".txt", ".xml", ".yaml", ".yml", ".tsv", ".ndjson"]);
    this.filesProcessed = 0;
    this.totalBytesRead = 0;
    this.cache = new CacheManager<string>(500, 60000);
  }

  get processedCount(): number { return this.filesProcessed; }
  get bytesRead(): string { return formatBytes(this.totalBytesRead); }

  public isSupportedFile(filename: string): boolean {
    return this.extensions.has(extname(filename).toLowerCase());
  }

  public addSupportedExtension(ext: string): void {
    this.extensions.add(ext.startsWith(".") ? ext : `.${ext}`);
  }

  public async readFileContent(filePath: string): Promise<string> {
    const abs = this.resolvePath(filePath);
    const cached = this.cache.get(abs);
    if (cached !== undefined) return cached;
    const content = await readFile(abs, "utf-8");
    this.totalBytesRead += Buffer.byteLength(content, "utf-8");
    this.filesProcessed++;
    this.cache.set(abs, content);
    return content;
  }

  public async readJsonFile<T>(filePath: string): Promise<T> {
    const content = await this.readFileContent(filePath);
    try { return JSON.parse(content) as T; }
    catch (e) { throw new Error(`JSON parse failed for ${filePath}: ${e instanceof Error ? e.message : e}`); }
  }

  public async writeJsonFile<T>(filePath: string, data: T): Promise<void> {
    const abs = this.resolvePath(filePath);
    await ensureDirectory(dirname(abs));
    const content = JSON.stringify(data, null, 2);
    await writeFile(abs, content, "utf-8");
    this.cache.set(abs, content);
    this.logger.info(`Written: ${abs}`, { bytes: Buffer.byteLength(content, "utf-8") });
  }

  public async listFiles(directory?: string, recursive = false): Promise<string[]> {
    const targetDir = directory ? this.resolvePath(directory) : this.baseDir;
    const results: string[] = [];
    const entries = await readdir(targetDir, { withFileTypes: true });
    for (const entry of entries) {
      const full = join(targetDir, entry.name);
      if (entry.isFile() && this.isSupportedFile(entry.name)) results.push(full);
      else if (entry.isDirectory() && recursive) results.push(...await this.listFiles(full, true));
    }
    return results.sort();
  }

  public async getFileStats(filePath: string): Promise<{ name: string; size: number; sizeFormatted: string; extension: string; modifiedAt: Date }> {
    const abs = this.resolvePath(filePath);
    const s = await stat(abs);
    return { name: basename(abs), size: s.size, sizeFormatted: formatBytes(s.size), extension: extname(abs), modifiedAt: s.mtime };
  }

  private resolvePath(filePath: string): string {
    return filePath.startsWith("/") ? filePath : join(this.baseDir, filePath);
  }

  public async deleteFile(filePath: string): Promise<boolean> {
    const abs = this.resolvePath(filePath);
    try { await unlink(abs); this.cache.delete(abs); return true; }
    catch { this.logger.warn(`Delete failed: ${abs}`); return false; }
  }

  public clearCache(): void { this.cache.clear(); }
}

// ============================================================================
// Async Standalone Functions
// ============================================================================

export async function loadPipelineFromFile(configPath: string): Promise<DataPipeline> {
  const proc = new FileProcessor(dirname(configPath));
  const config = await proc.readJsonFile<Partial<PipelineConfig>>(basename(configPath));
  Logger.getInstance().info("Loading pipeline from file", { path: configPath });
  return new DataPipeline(config);
}

export async function processDirectory(dirPath: string, pipeline: DataPipeline): Promise<{ total: number; processed: number; failed: number }> {
  const proc = new FileProcessor(dirPath);
  const files = await proc.listFiles(undefined, true);
  let total = 0, processed = 0, failed = 0;
  for (const file of files) {
    if (!file.endsWith(".json")) continue;
    total++;
    try {
      const data = await proc.readJsonFile<DataRecord>(file);
      if (await pipeline.processRecord(data)) processed++; else failed++;
    } catch { failed++; }
  }
  return { total, processed, failed };
}

async function encryptPayload(payload: string, key: Buffer, iv: Buffer): Promise<string> {
  const cipher = createCipheriv("aes-256-cbc", key, iv);
  return cipher.update(payload, "utf-8", "hex") + cipher.final("hex");
}

async function decryptPayload(encrypted: string, key: Buffer, iv: Buffer): Promise<string> {
  const decipher = createDecipheriv("aes-256-cbc", key, iv);
  return decipher.update(encrypted, "hex", "utf-8") + decipher.final("utf-8");
}

export async function createRecordFromRawData(source: string, rawData: Record<string, unknown>): Promise<DataRecord> {
  const id = generateRecordId();
  const now = new Date();
  const payloadStr = JSON.stringify(rawData);
  return {
    id, source, timestamp: now.getTime(), payload: rawData,
    metadata: { createdAt: now, updatedAt: now, checksum: computeChecksum(payloadStr), sizeBytes: Buffer.byteLength(payloadStr, "utf-8"), tags: [source, "raw"], priority: 5 },
    schemaVersion: "1.0.0",
  };
}

export async function aggregateMetrics(pipelines: DataPipeline[]): Promise<MetricsSnapshot> {
  const snaps = pipelines.map((p) => p.metrics);
  let totalProcessed = 0, totalFailed = 0, totalSkipped = 0, totalLatency = 0;
  let maxP95 = 0, maxP99 = 0, totalTput = 0, totalWorkers = 0, totalQueue = 0, maxUptime = 0;
  for (const s of snaps) {
    totalProcessed += s.recordsProcessed;
    totalFailed += s.recordsFailed;
    totalSkipped += s.recordsSkipped;
    totalLatency += s.averageLatencyMs * s.recordsProcessed;
    maxP95 = Math.max(maxP95, s.p95LatencyMs);
    maxP99 = Math.max(maxP99, s.p99LatencyMs);
    totalTput += s.throughputPerSecond;
    totalWorkers += s.activeWorkers;
    totalQueue += s.queueDepth;
    maxUptime = Math.max(maxUptime, s.uptime);
  }
  const total = totalProcessed + totalFailed;
  return {
    recordsProcessed: totalProcessed, recordsFailed: totalFailed, recordsSkipped: totalSkipped,
    averageLatencyMs: totalProcessed > 0 ? Math.round((totalLatency / totalProcessed) * 100) / 100 : 0,
    p95LatencyMs: maxP95, p99LatencyMs: maxP99,
    throughputPerSecond: Math.round(totalTput * 100) / 100,
    activeWorkers: totalWorkers, queueDepth: totalQueue,
    errorRate: total > 0 ? Math.round((totalFailed / total) * 10000) / 100 : 0,
    uptime: maxUptime,
  };
}

export async function exportMetricsToFile(metrics: MetricsSnapshot, outputPath: string): Promise<void> {
  const proc = new FileProcessor(dirname(outputPath));
  await proc.writeJsonFile(basename(outputPath), {
    exportedAt: new Date().toISOString(), metrics,
    formatted: {
      processed: `${metrics.recordsProcessed} records`, failed: `${metrics.recordsFailed} records`,
      avgLatency: `${metrics.averageLatencyMs}ms`, throughput: `${metrics.throughputPerSecond} rec/s`,
      errorRate: `${metrics.errorRate}%`, uptime: `${Math.round(metrics.uptime / 1000)}s`,
    },
  });
}

// ============================================================================
// Middleware Factories
// ============================================================================

export function createLoggingMiddleware(logger: Logger): MiddlewareFn {
  return async (req, _res, next) => {
    const t0 = Date.now();
    logger.info("Incoming", { method: req.method, path: req.path, requestId: req.requestId });
    await next();
    logger.info("Completed", { method: req.method, path: req.path, ms: Date.now() - t0 });
  };
}

export function createAuthMiddleware(validTokens: Set<string>): MiddlewareFn {
  const logger = new Logger("AuthMiddleware");
  return async (req, _res, next) => {
    const auth = req.headers["authorization"];
    if (!auth) { logger.warn("Missing auth header"); throw new Error("Unauthorized: Missing header"); }
    const parts = auth.split(" ");
    if (parts.length !== 2 || parts[0] !== "Bearer") throw new Error("Unauthorized: Invalid format");
    if (!validTokens.has(parts[1])) { logger.warn("Invalid token"); throw new Error("Unauthorized: Invalid token"); }
    await next();
  };
}

export function createRateLimitMiddleware(maxPerMinute: number): MiddlewareFn {
  const counts = new Map<string, number[]>();
  return async (req, _res, next) => {
    const ip = req.headers["x-forwarded-for"] ?? "unknown";
    const now = Date.now();
    let ts = counts.get(ip) ?? [];
    ts = ts.filter((t) => now - t < 60000);
    ts.push(now);
    counts.set(ip, ts);
    if (ts.length > maxPerMinute) throw new Error("Too Many Requests");
    await next();
  };
}

function createCorsMiddleware(origins: string[]): MiddlewareFn {
  return async (req, res, next) => {
    const origin = req.headers["origin"];
    if (origin && origins.includes(origin)) {
      res.headers["Access-Control-Allow-Origin"] = origin;
      res.headers["Access-Control-Allow-Methods"] = "GET, POST, PUT, DELETE, OPTIONS";
      res.headers["Access-Control-Allow-Headers"] = "Content-Type, Authorization";
    }
    if (req.method === HttpMethod.OPTIONS) { res.statusCode = 204; return; }
    await next();
  };
}

// ============================================================================
// Route Handler Factories
// ============================================================================

export function createHealthCheckHandler(): RouteHandler {
  const t0 = Date.now();
  return async (_req, res) => {
    res.statusCode = 200;
    res.headers["Content-Type"] = "application/json";
    res.body = { status: "healthy", uptime: Date.now() - t0, timestamp: new Date().toISOString(), version: "1.0.0" };
    return res;
  };
}

function createRecordHandler(pipeline: DataPipeline): RouteHandler {
  const logger = new Logger("RecordHandler");
  return async (req, res) => {
    if (!req.body || typeof req.body !== "object") return buildErrorResponse(400, "Body required");
    const body = req.body as Record<string, unknown>;
    try {
      const record = await createRecordFromRawData((body.source as string) ?? "api", (body.payload as Record<string, unknown>) ?? {});
      const ok = await pipeline.processRecord(record);
      res.statusCode = ok ? 201 : 422;
      res.headers["Content-Type"] = "application/json";
      res.body = { id: record.id, status: ok ? "processed" : "failed" };
      return res;
    } catch (error) {
      logger.error("Failed", error as Error);
      return buildErrorResponse(500, error instanceof Error ? error.message : "Unknown");
    }
  };
}

function createMetricsHandler(pipelines: DataPipeline[]): RouteHandler {
  return async (_req, res) => {
    const agg = await aggregateMetrics(pipelines);
    res.statusCode = 200;
    res.headers["Content-Type"] = "application/json";
    res.body = { timestamp: new Date().toISOString(), aggregated: agg, pipelines: pipelines.map((p) => ({ stage: p.stage, metrics: p.metrics })) };
    return res;
  };
}

// ============================================================================
// Summary, Search, Pagination, Batch
// ============================================================================

export function summarizeRecords(records: DataRecord[]): {
  totalCount: number; uniqueSources: string[]; totalSizeBytes: number; totalSizeFormatted: string;
  oldestTimestamp: number; newestTimestamp: number; averageSizeBytes: number; tagDistribution: Record<string, number>;
} {
  if (records.length === 0) return { totalCount: 0, uniqueSources: [], totalSizeBytes: 0, totalSizeFormatted: "0 B", oldestTimestamp: 0, newestTimestamp: 0, averageSizeBytes: 0, tagDistribution: {} };
  const sources = new Set<string>();
  let totalSize = 0, oldest = Infinity, newest = -Infinity;
  const tags: Record<string, number> = {};
  for (const r of records) {
    sources.add(r.source);
    totalSize += r.metadata.sizeBytes;
    oldest = Math.min(oldest, r.timestamp);
    newest = Math.max(newest, r.timestamp);
    for (const t of r.metadata.tags) tags[t] = (tags[t] ?? 0) + 1;
  }
  return { totalCount: records.length, uniqueSources: Array.from(sources).sort(), totalSizeBytes: totalSize, totalSizeFormatted: formatBytes(totalSize), oldestTimestamp: oldest, newestTimestamp: newest, averageSizeBytes: Math.round(totalSize / records.length), tagDistribution: tags };
}

export function searchRecords(records: DataRecord[], query: string, fields: string[] = ["source", "id"]): DataRecord[] {
  const lq = query.toLowerCase();
  return records.filter((record) => {
    for (const field of fields) {
      let val: unknown;
      if (field.includes(".")) {
        const parts = field.split(".");
        let cur: unknown = record;
        for (const p of parts) { cur = (cur && typeof cur === "object") ? (cur as Record<string, unknown>)[p] : undefined; }
        val = cur;
      } else {
        val = (record as unknown as Record<string, unknown>)[field];
      }
      if (val !== undefined && val !== null && String(val).toLowerCase().includes(lq)) return true;
    }
    return false;
  });
}

export function paginateResults<T>(items: T[], page: number, pageSize: number): {
  data: T[]; page: number; pageSize: number; totalItems: number; totalPages: number; hasNextPage: boolean; hasPreviousPage: boolean;
} {
  const p = Math.max(1, Math.floor(page));
  const ps = clampValue(Math.floor(pageSize), 1, 1000);
  const totalPages = Math.ceil(items.length / ps);
  const start = (p - 1) * ps;
  return { data: items.slice(start, Math.min(start + ps, items.length)), page: p, pageSize: ps, totalItems: items.length, totalPages, hasNextPage: p < totalPages, hasPreviousPage: p > 1 };
}

export async function processBatchWithConcurrency<TIn, TOut>(
  items: TIn[], processor: (item: TIn, index: number) => Promise<TOut>, concurrency: number
): Promise<{ results: TOut[]; errors: Array<{ index: number; error: Error }> }> {
  const results: TOut[] = new Array(items.length);
  const errors: Array<{ index: number; error: Error }> = [];
  let nextIdx = 0;
  const worker = async (): Promise<void> => {
    while (nextIdx < items.length) {
      const i = nextIdx++;
      try { results[i] = await processor(items[i], i); }
      catch (e) { errors.push({ index: i, error: e instanceof Error ? e : new Error(String(e)) }); }
    }
  };
  await Promise.all(Array.from({ length: Math.min(concurrency, items.length) }, () => worker()));
  return { results, errors };
}

export async function streamRecordsToFile(records: DataRecord[], outputPath: string, format: "json" | "ndjson" | "csv"): Promise<{ bytesWritten: number; recordCount: number }> {
  let content: string;
  switch (format) {
    case "json": content = JSON.stringify(records, null, 2); break;
    case "ndjson": content = records.map((r) => JSON.stringify(r)).join("\n"); break;
    case "csv": {
      if (records.length === 0) { content = ""; break; }
      const hdrs = ["id", "source", "timestamp", "schemaVersion"];
      const rows = records.map((r) => hdrs.map((h) => { const v = String((r as unknown as Record<string, unknown>)[h] ?? ""); return v.includes(",") ? `"${v}"` : v; }).join(","));
      content = [hdrs.join(","), ...rows].join("\n");
      break;
    }
    default: throw new Error(`Unsupported format: ${format}`);
  }
  await writeFile(outputPath, content, "utf-8");
  return { bytesWritten: Buffer.byteLength(content, "utf-8"), recordCount: records.length };
}

// ============================================================================
// Error Handling
// ============================================================================

export class PipelineError extends Error {
  public readonly code: string;
  public readonly statusCode: number;
  public readonly context: Record<string, unknown>;
  public readonly timestamp: number;

  constructor(message: string, code: string, statusCode = 500, context: Record<string, unknown> = {}) {
    super(message);
    this.name = "PipelineError";
    this.code = code;
    this.statusCode = statusCode;
    this.context = context;
    this.timestamp = Date.now();
  }

  public toJSON(): Record<string, unknown> {
    return { name: this.name, message: this.message, code: this.code, statusCode: this.statusCode, context: this.context, timestamp: this.timestamp, stack: this.stack };
  }
}

export function createValidationError(field: string, message: string, actual?: unknown): PipelineError {
  return new PipelineError(`Validation error at '${field}': ${message}`, "VALIDATION_ERROR", 400, { field, actual });
}

export function createNotFoundError(type: string, id: string): PipelineError {
  return new PipelineError(`${type} not found: ${id}`, "NOT_FOUND", 404, { type, id });
}

function createTimeoutError(op: string, ms: number): PipelineError {
  return new PipelineError(`'${op}' timed out after ${ms}ms`, "TIMEOUT", 408, { op, ms });
}

export function isRetryableError(error: unknown): boolean {
  if (error instanceof PipelineError) return ["TIMEOUT", "SERVICE_UNAVAILABLE", "RATE_LIMITED"].includes(error.code);
  if (error instanceof Error) return ["ECONNRESET", "ECONNREFUSED", "ETIMEDOUT", "EPIPE"].some((p) => error.message.includes(p));
  return false;
}

// ============================================================================
// Validation Helpers
// ============================================================================

export function validateEmailAddress(email: string): ValidationResult {
  const errors: ValidationError[] = [];
  const warnings: string[] = [];
  if (!email || email.trim().length === 0) {
    errors.push({ code: "EMPTY", message: "Email cannot be empty", fieldPath: "email", severity: "error" });
    return { valid: false, errors, warnings, fieldPath: "email" };
  }
  if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) {
    errors.push({ code: "INVALID_FORMAT", message: "Invalid email format", fieldPath: "email", expectedType: "email", actualValue: email, severity: "error" });
  }
  if (email.length > 254) {
    errors.push({ code: "TOO_LONG", message: "Email exceeds 254 chars", fieldPath: "email", severity: "error" });
  }
  if (email !== email.toLowerCase()) warnings.push("Email contains uppercase");
  return { valid: errors.length === 0, errors, warnings, fieldPath: "email" };
}

export function validateDateRange(start: Date, end: Date): ValidationResult {
  const errors: ValidationError[] = [];
  const warnings: string[] = [];
  if (isNaN(start.getTime())) errors.push({ code: "INVALID_START", message: "Invalid start date", fieldPath: "startDate", severity: "error" });
  if (isNaN(end.getTime())) errors.push({ code: "INVALID_END", message: "Invalid end date", fieldPath: "endDate", severity: "error" });
  if (errors.length === 0 && start.getTime() > end.getTime()) {
    errors.push({ code: "INVALID_RANGE", message: "Start must be <= end", fieldPath: "dateRange", severity: "error" });
  }
  if (errors.length === 0 && (end.getTime() - start.getTime()) / 86400000 > 365) {
    warnings.push("Range spans more than one year");
  }
  return { valid: errors.length === 0, errors, warnings, fieldPath: "dateRange" };
}

function validateNumericRange(value: number, field: string, min?: number, max?: number): ValidationResult {
  const errors: ValidationError[] = [];
  if (typeof value !== "number" || isNaN(value)) {
    errors.push({ code: "NAN", message: `'${field}' must be a number`, fieldPath: field, severity: "error" });
    return { valid: false, errors, warnings: [], fieldPath: field };
  }
  if (!isFinite(value)) errors.push({ code: "NOT_FINITE", message: `'${field}' must be finite`, fieldPath: field, severity: "error" });
  if (min !== undefined && value < min) errors.push({ code: "BELOW_MIN", message: `'${field}' must be >= ${min}`, fieldPath: field, severity: "error" });
  if (max !== undefined && value > max) errors.push({ code: "ABOVE_MAX", message: `'${field}' must be <= ${max}`, fieldPath: field, severity: "error" });
  return { valid: errors.length === 0, errors, warnings: [], fieldPath: field };
}

export function validatePipelineConfig(config: Partial<PipelineConfig>): ValidationResult {
  const errors: ValidationError[] = [];
  const warnings: string[] = [];
  if (config.name !== undefined) {
    if (typeof config.name !== "string" || config.name.trim().length === 0)
      errors.push({ code: "INVALID_NAME", message: "Name must be non-empty string", fieldPath: "name", severity: "error" });
    else if (config.name.length > 128)
      errors.push({ code: "NAME_TOO_LONG", message: "Name > 128 chars", fieldPath: "name", severity: "error" });
  }
  if (config.maxConcurrency !== undefined) errors.push(...validateNumericRange(config.maxConcurrency, "maxConcurrency", 1, 64).errors);
  if (config.batchSize !== undefined) errors.push(...validateNumericRange(config.batchSize, "batchSize", 1, 10000).errors);
  if (config.retryAttempts !== undefined) errors.push(...validateNumericRange(config.retryAttempts, "retryAttempts", 0, 10).errors);
  if (config.timeoutMs !== undefined) {
    if (config.timeoutMs < 1000) warnings.push("Timeout < 1000ms may cause frequent timeouts");
    errors.push(...validateNumericRange(config.timeoutMs, "timeoutMs", 100, 600000).errors);
  }
  if (config.retryDelayMs !== undefined) errors.push(...validateNumericRange(config.retryDelayMs, "retryDelayMs", 10, 60000).errors);
  return { valid: errors.length === 0, errors, warnings, fieldPath: "config" };
}

// ============================================================================
// Reporting
// ============================================================================

export async function generateProcessingReport(pipeline: DataPipeline, outputPath: string): Promise<void> {
  const m = pipeline.metrics;
  const total = m.recordsProcessed + m.recordsFailed;
  const report = {
    generatedAt: new Date().toISOString(),
    stage: pipeline.stage,
    summary: { processed: m.recordsProcessed, failed: m.recordsFailed, successRate: total > 0 ? Math.round((m.recordsProcessed / total) * 10000) / 100 : 100, errorRate: m.errorRate },
    performance: { avgLatency: `${m.averageLatencyMs}ms`, p95: `${m.p95LatencyMs}ms`, p99: `${m.p99LatencyMs}ms`, throughput: `${m.throughputPerSecond} rec/s` },
    resources: { workers: m.activeWorkers, queueDepth: m.queueDepth, uptime: `${Math.round(m.uptime / 1000)}s` },
  };
  await ensureDirectory(dirname(outputPath));
  await writeFile(outputPath, JSON.stringify(report, null, 2), "utf-8");
}

export function formatMetricsForDisplay(m: MetricsSnapshot): string {
  return [
    "=== Pipeline Metrics ===", "",
    "Records:", `  Processed: ${m.recordsProcessed}`, `  Failed:    ${m.recordsFailed}`, `  Skipped:   ${m.recordsSkipped}`, `  Error Rate: ${m.errorRate}%`, "",
    "Latency:", `  Average: ${m.averageLatencyMs}ms`, `  P95:     ${m.p95LatencyMs}ms`, `  P99:     ${m.p99LatencyMs}ms`, "",
    "Throughput:", `  ${m.throughputPerSecond} records/sec`, "",
    "Resources:", `  Workers: ${m.activeWorkers}`, `  Queue:   ${m.queueDepth}`, `  Uptime:  ${Math.round(m.uptime / 1000)}s`,
  ].join("\n");
}

// ============================================================================
// Encryption Utilities
// ============================================================================

export function generateEncryptionKey(): Buffer { return randomBytes(32); }
export function generateInitializationVector(): Buffer { return randomBytes(16); }

export async function encryptRecord(record: DataRecord, key: Buffer, iv: Buffer): Promise<{ encryptedPayload: string; metadata: RecordMetadata }> {
  const encrypted = await encryptPayload(JSON.stringify(record.payload), key, iv);
  return { encryptedPayload: encrypted, metadata: record.metadata };
}

export async function decryptRecord(encrypted: string, key: Buffer, iv: Buffer): Promise<Record<string, unknown>> {
  const decrypted = await decryptPayload(encrypted, key, iv);
  return JSON.parse(decrypted) as Record<string, unknown>;
}

export function convertCsvRowsToRecords(rows: Record<string, string>[], source: string): DataRecord[] {
  return rows.map((row) => {
    const id = generateRecordId();
    const now = new Date();
    const ps = JSON.stringify(row);
    return { id, source, timestamp: now.getTime(), payload: row as unknown as Record<string, unknown>,
      metadata: { createdAt: now, updatedAt: now, checksum: computeChecksum(ps), sizeBytes: Buffer.byteLength(ps, "utf-8"), tags: [source, "csv-import"], priority: 5 },
      schemaVersion: "1.0.0" };
  });
}

// ============================================================================
// Bootstrap and Default Export
// ============================================================================

export async function bootstrapApplication(configOverrides: Partial<PipelineConfig> = {}): Promise<{
  pipeline: DataPipeline; router: ApiRouter; cache: CacheManager; logger: Logger;
}> {
  const logger = new Logger("Bootstrap", LogLevel.INFO);
  logger.info("Starting bootstrap");
  const pipeline = new DataPipeline(configOverrides);
  await pipeline.start();
  const router = new ApiRouter();
  const cache = new CacheManager(5000, 600000);
  router.use(createLoggingMiddleware(logger));
  router.use(createCorsMiddleware(["http://localhost:3000"]));
  router.use(createRateLimitMiddleware(120));
  router.get("/health", createHealthCheckHandler());
  router.post("/records", createRecordHandler(pipeline));
  router.get("/metrics", createMetricsHandler([pipeline]));
  router.get("/records/summary", async (_req, res) => {
    const m = pipeline.metrics;
    res.statusCode = 200;
    res.headers["Content-Type"] = "application/json";
    res.body = { processed: m.recordsProcessed, failed: m.recordsFailed, throughput: m.throughputPerSecond };
    return res;
  });
  router.get("/cache/stats", async (_req, res) => {
    res.statusCode = 200;
    res.headers["Content-Type"] = "application/json";
    res.body = cache.statistics;
    return res;
  });
  logger.info("Bootstrap complete", { routes: router.routeCount });
  return { pipeline, router, cache, logger };
}

export default async function initializeService(configPath?: string): Promise<{
  pipeline: DataPipeline; router: ApiRouter; logger: Logger;
}> {
  const logger = new Logger("ServiceInit", LogLevel.INFO);
  logger.info("Initializing service");
  let config: Partial<PipelineConfig> = {};
  if (configPath) {
    try {
      const proc = new FileProcessor(dirname(configPath));
      config = await proc.readJsonFile<Partial<PipelineConfig>>(basename(configPath));
      logger.info("Loaded config", { path: configPath });
    } catch (e) {
      logger.warn("Config load failed, using defaults", { error: e instanceof Error ? e.message : String(e) });
    }
  }
  const vr = validatePipelineConfig(config);
  if (!vr.valid) throw new PipelineError(`Invalid config: ${vr.errors.map((e) => e.message).join("; ")}`, "CONFIG_ERROR", 500);
  for (const w of vr.warnings) logger.warn(`Config warning: ${w}`);
  const { pipeline, router } = await bootstrapApplication(config);
  logger.info("Service ready", { stage: pipeline.stage, routes: router.routeCount });
  return { pipeline, router, logger };
}

// ============================================================================
// Class: TaskScheduler
// ============================================================================

export class TaskScheduler {
  private readonly tasks: Map<string, ScheduledTask>;
  private readonly logger: Logger;
  private readonly maxConcurrentTasks: number;
  private activeTasks: number;
  private completedCount: number;
  private failedCount: number;

  constructor(maxConcurrent: number = 8) {
    this.tasks = new Map();
    this.logger = new Logger("TaskScheduler");
    this.maxConcurrentTasks = maxConcurrent;
    this.activeTasks = 0;
    this.completedCount = 0;
    this.failedCount = 0;
  }

  get pending(): number {
    let count = 0;
    for (const task of this.tasks.values()) {
      if (task.status === "pending") count++;
    }
    return count;
  }

  get active(): number {
    return this.activeTasks;
  }

  get stats(): { pending: number; active: number; completed: number; failed: number; total: number } {
    return {
      pending: this.pending,
      active: this.activeTasks,
      completed: this.completedCount,
      failed: this.failedCount,
      total: this.tasks.size,
    };
  }

  public schedule(
    taskId: string,
    execute: () => Promise<void>,
    priority: number = 5
  ): void {
    if (this.tasks.has(taskId)) {
      this.logger.warn(`Task already exists: ${taskId}`);
      return;
    }
    const task: ScheduledTask = {
      id: taskId,
      execute,
      priority: clampValue(priority, 1, 10),
      status: "pending",
      scheduledAt: Date.now(),
      startedAt: null,
      completedAt: null,
      error: null,
    };
    this.tasks.set(taskId, task);
    this.logger.debug(`Scheduled task: ${taskId}`, { priority });
  }

  public async runNext(): Promise<boolean> {
    if (this.activeTasks >= this.maxConcurrentTasks) {
      this.logger.debug("Max concurrency reached, deferring");
      return false;
    }

    const pendingTasks = Array.from(this.tasks.values())
      .filter((t) => t.status === "pending")
      .sort((a, b) => b.priority - a.priority);

    if (pendingTasks.length === 0) {
      return false;
    }

    const task = pendingTasks[0];
    task.status = "running";
    task.startedAt = Date.now();
    this.activeTasks++;

    try {
      await task.execute();
      task.status = "completed";
      task.completedAt = Date.now();
      this.completedCount++;
      this.logger.info(`Task completed: ${task.id}`, {
        durationMs: task.completedAt - (task.startedAt ?? task.scheduledAt),
      });
    } catch (error) {
      task.status = "failed";
      task.completedAt = Date.now();
      task.error = error instanceof Error ? error.message : String(error);
      this.failedCount++;
      this.logger.error(`Task failed: ${task.id}`, error as Error);
    } finally {
      this.activeTasks--;
    }

    return true;
  }

  public async runAll(): Promise<{ completed: number; failed: number }> {
    let completed = 0;
    let failed = 0;

    while (this.pending > 0) {
      const promises: Promise<boolean>[] = [];
      const available = this.maxConcurrentTasks - this.activeTasks;
      for (let i = 0; i < Math.min(available, this.pending); i++) {
        promises.push(this.runNext());
      }
      const results = await Promise.all(promises);
      for (const ran of results) {
        if (!ran) break;
      }
    }

    for (const task of this.tasks.values()) {
      if (task.status === "completed") completed++;
      if (task.status === "failed") failed++;
    }

    return { completed, failed };
  }

  public cancel(taskId: string): boolean {
    const task = this.tasks.get(taskId);
    if (!task || task.status !== "pending") return false;
    task.status = "cancelled";
    this.logger.info(`Task cancelled: ${taskId}`);
    return true;
  }

  public getTask(taskId: string): ScheduledTask | undefined {
    return this.tasks.get(taskId);
  }

  public clearCompleted(): number {
    let removed = 0;
    for (const [id, task] of this.tasks.entries()) {
      if (task.status === "completed" || task.status === "failed" || task.status === "cancelled") {
        this.tasks.delete(id);
        removed++;
      }
    }
    return removed;
  }
}

interface ScheduledTask {
  id: string;
  execute: () => Promise<void>;
  priority: number;
  status: "pending" | "running" | "completed" | "failed" | "cancelled";
  scheduledAt: number;
  startedAt: number | null;
  completedAt: number | null;
  error: string | null;
}

// ============================================================================
// Class: ConnectionPool
// ============================================================================

export class ConnectionPool {
  private readonly connections: PooledConnection[];
  private readonly maxConnections: number;
  private readonly logger: Logger;
  private readonly idleTimeoutMs: number;
  private totalCreated: number;
  private totalDestroyed: number;

  constructor(maxConnections: number = 10, idleTimeoutMs: number = 30000) {
    this.connections = [];
    this.maxConnections = maxConnections;
    this.logger = new Logger("ConnectionPool");
    this.idleTimeoutMs = idleTimeoutMs;
    this.totalCreated = 0;
    this.totalDestroyed = 0;
  }

  get size(): number {
    return this.connections.length;
  }

  get availableCount(): number {
    return this.connections.filter((c) => !c.inUse).length;
  }

  get stats(): { total: number; available: number; inUse: number; created: number; destroyed: number } {
    const inUse = this.connections.filter((c) => c.inUse).length;
    return {
      total: this.connections.length,
      available: this.availableCount,
      inUse,
      created: this.totalCreated,
      destroyed: this.totalDestroyed,
    };
  }

  public async acquire(): Promise<PooledConnection> {
    const available = this.connections.find((c) => !c.inUse);
    if (available) {
      available.inUse = true;
      available.lastUsedAt = Date.now();
      available.useCount++;
      this.logger.debug(`Connection acquired: ${available.id}`);
      return available;
    }

    if (this.connections.length < this.maxConnections) {
      const conn = this.createConnection();
      conn.inUse = true;
      conn.lastUsedAt = Date.now();
      conn.useCount++;
      this.connections.push(conn);
      this.logger.info(`New connection created: ${conn.id}`, { poolSize: this.connections.length });
      return conn;
    }

    this.logger.warn("Connection pool exhausted, waiting");
    await sleepMs(100);
    return this.acquire();
  }

  public release(connectionId: string): void {
    const conn = this.connections.find((c) => c.id === connectionId);
    if (!conn) {
      this.logger.warn(`Unknown connection: ${connectionId}`);
      return;
    }
    conn.inUse = false;
    conn.lastUsedAt = Date.now();
    this.logger.debug(`Connection released: ${connectionId}`);
  }

  private createConnection(): PooledConnection {
    this.totalCreated++;
    return {
      id: `conn_${generateRecordId()}`,
      inUse: false,
      createdAt: Date.now(),
      lastUsedAt: Date.now(),
      useCount: 0,
    };
  }

  public async destroy(connectionId: string): Promise<boolean> {
    const idx = this.connections.findIndex((c) => c.id === connectionId);
    if (idx === -1) return false;
    this.connections.splice(idx, 1);
    this.totalDestroyed++;
    this.logger.info(`Connection destroyed: ${connectionId}`);
    return true;
  }

  public async pruneIdle(): Promise<number> {
    const now = Date.now();
    let pruned = 0;
    for (let i = this.connections.length - 1; i >= 0; i--) {
      const conn = this.connections[i];
      if (!conn.inUse && now - conn.lastUsedAt > this.idleTimeoutMs) {
        this.connections.splice(i, 1);
        this.totalDestroyed++;
        pruned++;
      }
    }
    if (pruned > 0) this.logger.info(`Pruned ${pruned} idle connections`);
    return pruned;
  }

  public async drainAll(): Promise<void> {
    this.logger.info("Draining all connections", { count: this.connections.length });
    for (const conn of this.connections) {
      conn.inUse = false;
    }
    this.totalDestroyed += this.connections.length;
    this.connections.length = 0;
    this.logger.info("All connections drained");
  }
}

interface PooledConnection {
  id: string;
  inUse: boolean;
  createdAt: number;
  lastUsedAt: number;
  useCount: number;
}

// ============================================================================
// Additional Standalone & Async Functions
// ============================================================================

export function groupRecordsBySource(records: DataRecord[]): Map<string, DataRecord[]> {
  const groups = new Map<string, DataRecord[]>();
  for (const record of records) {
    const key = record.source.toLowerCase();
    const list = groups.get(key) ?? [];
    list.push(record);
    groups.set(key, list);
  }
  return groups;
}

export async function processFileWithPipeline(
  filePath: string,
  pipeline: DataPipeline,
  source: string
): Promise<{ success: boolean; recordId?: string; error?: string }> {
  const processor = new FileProcessor(dirname(filePath));
  try {
    const rawData = await processor.readJsonFile<Record<string, unknown>>(basename(filePath));
    const record = await createRecordFromRawData(source, rawData);
    const ok = await pipeline.processRecord(record);
    return ok
      ? { success: true, recordId: record.id }
      : { success: false, error: "Pipeline rejected record" };
  } catch (error) {
    return { success: false, error: error instanceof Error ? error.message : String(error) };
  }
}

export async function batchImportCsv(
  csvPath: string,
  pipeline: DataPipeline,
  source: string
): Promise<{ imported: number; failed: number; total: number }> {
  const processor = new FileProcessor(dirname(csvPath));
  const content = await processor.readFileContent(basename(csvPath));
  const { rows } = parseCsvContent(content);
  const records = convertCsvRowsToRecords(rows, source);
  const result = await pipeline.processBatch(records);
  return { imported: result.succeeded, failed: result.failed, total: records.length };
}

export async function healthCheck(
  pipeline: DataPipeline,
  cache: CacheManager,
  pool?: ConnectionPool
): Promise<{
  status: "healthy" | "degraded" | "unhealthy";
  components: Record<string, { status: string; details: Record<string, unknown> }>;
}> {
  const components: Record<string, { status: string; details: Record<string, unknown> }> = {};

  const pipelineMetrics = pipeline.metrics;
  components.pipeline = {
    status: pipeline.stage === PipelineStage.FAILED ? "unhealthy" : "healthy",
    details: {
      stage: pipeline.stage,
      processed: pipelineMetrics.recordsProcessed,
      errorRate: pipelineMetrics.errorRate,
    },
  };

  const cacheStats = cache.statistics;
  components.cache = {
    status: cacheStats.size > cacheStats.maxSize * 0.95 ? "degraded" : "healthy",
    details: {
      size: cacheStats.size,
      hitRate: cacheStats.hitRate,
      evictions: cacheStats.evictions,
    },
  };

  if (pool) {
    const poolStats = pool.stats;
    components.connectionPool = {
      status: poolStats.available === 0 ? "degraded" : "healthy",
      details: {
        total: poolStats.total,
        available: poolStats.available,
        inUse: poolStats.inUse,
      },
    };
  }

  const statuses = Object.values(components).map((c) => c.status);
  let overall: "healthy" | "degraded" | "unhealthy" = "healthy";
  if (statuses.includes("unhealthy")) overall = "unhealthy";
  else if (statuses.includes("degraded")) overall = "degraded";

  return { status: overall, components };
}

export function buildFilterPredicate(
  filters: Record<string, string | number | boolean>
): (record: DataRecord) => boolean {
  return (record: DataRecord): boolean => {
    for (const [key, expected] of Object.entries(filters)) {
      let actual: unknown;
      if (key.includes(".")) {
        const parts = key.split(".");
        let cur: unknown = record;
        for (const p of parts) {
          if (cur && typeof cur === "object") {
            cur = (cur as Record<string, unknown>)[p];
          } else {
            cur = undefined;
            break;
          }
        }
        actual = cur;
      } else {
        actual = (record as unknown as Record<string, unknown>)[key];
      }
      if (String(actual) !== String(expected)) return false;
    }
    return true;
  };
}

export async function runDiagnostics(
  pipeline: DataPipeline,
  scheduler: TaskScheduler,
  pool: ConnectionPool
): Promise<Record<string, unknown>> {
  const logger = new Logger("Diagnostics");
  logger.info("Running diagnostics");

  const pipelineMetrics = pipeline.metrics;
  const schedulerStats = scheduler.stats;
  const poolStats = pool.stats;

  const diagnostics: Record<string, unknown> = {
    timestamp: new Date().toISOString(),
    pipeline: {
      stage: pipeline.stage,
      metrics: pipelineMetrics,
      healthy: pipelineMetrics.errorRate < 10,
    },
    scheduler: {
      ...schedulerStats,
      healthy: schedulerStats.failed < schedulerStats.completed * 0.1,
    },
    pool: {
      ...poolStats,
      healthy: poolStats.available > 0 || poolStats.total < pool.size,
    },
    memory: {
      heapUsed: process.memoryUsage().heapUsed,
      heapTotal: process.memoryUsage().heapTotal,
      rss: process.memoryUsage().rss,
    },
  };

  logger.info("Diagnostics complete", { healthy: true });
  return diagnostics;
}

function extractFieldValues(records: DataRecord[], fieldPath: string): unknown[] {
  const values: unknown[] = [];
  for (const record of records) {
    const parts = fieldPath.split(".");
    let current: unknown = record;
    for (const part of parts) {
      if (current && typeof current === "object") {
        current = (current as Record<string, unknown>)[part];
      } else {
        current = undefined;
        break;
      }
    }
    if (current !== undefined) values.push(current);
  }
  return values;
}

export async function createBulkRecords(
  count: number,
  source: string,
  templatePayload: Record<string, unknown>
): Promise<DataRecord[]> {
  const records: DataRecord[] = [];
  for (let i = 0; i < count; i++) {
    const payload = { ...templatePayload, index: i, batchId: generateRecordId() };
    const record = await createRecordFromRawData(source, payload);
    records.push(record);
  }
  return records;
}

export function computeRecordDiff(
  before: DataRecord,
  after: DataRecord
): { field: string; before: unknown; after: unknown }[] {
  const diffs: { field: string; before: unknown; after: unknown }[] = [];
  const flatBefore = flattenObject(before as unknown as Record<string, unknown>);
  const flatAfter = flattenObject(after as unknown as Record<string, unknown>);
  const allKeys = new Set([...Object.keys(flatBefore), ...Object.keys(flatAfter)]);
  for (const key of allKeys) {
    const bVal = flatBefore[key];
    const aVal = flatAfter[key];
    if (JSON.stringify(bVal) !== JSON.stringify(aVal)) {
      diffs.push({ field: key, before: bVal, after: aVal });
    }
  }
  return diffs;
}
