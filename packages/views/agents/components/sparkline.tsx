'use client';

interface ActivityBucketLike {
  total: number;
  failed: number;
}

interface SparklineProps {
  buckets: readonly ActivityBucketLike[];
  width: number;
  height: number;
  className?: string;
}

const SUCCESS_FILL = 'var(--color-brand)';
const SUCCESS_OPACITY = 0.6;
const FAILED_FILL = 'var(--color-destructive)';
const BASELINE_FILL = 'var(--color-muted-foreground)';
const BASELINE_OPACITY = 0.25;

export function Sparkline({ buckets, width, height, className }: SparklineProps) {
  const n = buckets.length;
  if (n === 0) {
    return (
      <svg
        width={width}
        height={height}
        viewBox={`0 0 ${width} ${height}`}
        className={className}
        aria-hidden
      />
    );
  }

  const gap = 1;
  const colWidth = Math.max(1, Math.floor((width - gap * (n - 1)) / n));
  const usedWidth = colWidth * n + gap * (n - 1);
  const offsetX = Math.floor((width - usedWidth) / 2);

  let maxTotal = 0;
  for (const b of buckets) if (b.total > maxTotal) maxTotal = b.total;
  const scaleDenominator = Math.max(1, maxTotal);

  const baselineY = height - 1;
  const usableH = height - 1;

  return (
    <svg
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      className={className}
      aria-hidden
    >
      {/* Faint floor — a row with zero history still reads as "a row",
          not a missing cell. */}
      <rect
        x={0}
        y={baselineY}
        width={width}
        height={1}
        fill={BASELINE_FILL}
        fillOpacity={BASELINE_OPACITY}
      />
      {buckets.map((b, i) => {
        if (b.total === 0) return null;
        const x = offsetX + i * (colWidth + gap);
        const totalH = Math.max(1, Math.round((usableH * b.total) / scaleDenominator));
        const failedH =
          b.failed > 0
            ? Math.min(totalH, Math.max(1, Math.round((usableH * b.failed) / scaleDenominator)))
            : 0;
        const successH = totalH - failedH;
        const colTop = baselineY - totalH;
        return (
          <g key={i}>
            {successH > 0 && (
              <rect
                x={x}
                y={colTop + failedH}
                width={colWidth}
                height={successH}
                fill={SUCCESS_FILL}
                fillOpacity={SUCCESS_OPACITY}
              />
            )}
            {failedH > 0 && (
              <rect x={x} y={colTop} width={colWidth} height={failedH} fill={FAILED_FILL} />
            )}
          </g>
        );
      })}
    </svg>
  );
}
