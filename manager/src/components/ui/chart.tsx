import { useMemo } from 'react';
import { cn } from '@/lib/cn';

interface SimpleChartProps {
  data: number[];
  height?: number;
  color?: string;
  className?: string;
  showArea?: boolean;
  title?: string;
  unit?: string;
}

export function SimpleChart({ data, height = 60, color = 'currentColor', className, showArea = true, title, unit }: SimpleChartProps) {
  // Ensure at least one data point to avoid NaN in calculations
  const safeData = data.length > 0 ? data : [0];
  // Dark mode support via CSS variables
  const isDark = document.documentElement.classList.contains('dark');
  const strokeColor = isDark ? color : color;
  const areaOpacity = isDark ? 0.15 : 0.1;
  // Accessibility: aria-label for screen readers
  const ariaLabel = title || (unit ? `Gráfico de ${unit}` : 'Gráfico');
  const points = useMemo(() => {
    if (data.length === 0) return { pathData: '', areaPath: '', max: 0 };
    const max = Math.max(...safeData, 1);
    const min = Math.min(...safeData, 0);
    const range = max - min || 1;
    const width = 100;
    const stepX = width / (data.length - 1 || 1);

    const pathData = safeData.map((val, i) => {
      const x = i * stepX;
      const normalizedY = (val - min) / range;
      const y = height - normalizedY * height;
      return `${i === 0 ? 'M' : 'L'} ${x},${y}`;
    }).join(' ');

    const areaPath = showArea
      ? `${pathData} L ${width},${height} L 0,${height} Z`
      : '';

    return { pathData, areaPath, max };
  }, [data, height, showArea]);

  const maxVal = points.max;
  return (
    <div className={cn("relative w-full", className)} style={{ height }}>
      {title && (
        <div className="absolute left-0 top-0 text-[10px] font-medium text-muted opacity-70">
          {title}{unit && ` (${maxVal}${unit})`}
        </div>
      )}
      <svg viewBox={`0 0 100 ${height}`} preserveAspectRatio="none" className="h-full w-full overflow-visible" role="img" aria-label={ariaLabel}>
        {showArea && (
          <path d={points.areaPath} fill={color} fillOpacity={areaOpacity} />
        )}
        <path d={points.pathData} fill="none" stroke={strokeColor} strokeWidth="2" vectorEffect="non-scaling-stroke" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
    </div>
  );
}
