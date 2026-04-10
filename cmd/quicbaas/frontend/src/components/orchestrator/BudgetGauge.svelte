<!--
  BudgetGauge renders an ECharts gauge showing budget percent used with
  color zones: green (0-60%), yellow (60-80%), red (80-100%).
-->
<script lang="ts">
  import * as echarts from 'echarts/core';
  import { GaugeChart } from 'echarts/charts';
  import { CanvasRenderer } from 'echarts/renderers';
  import { TitleComponent, TooltipComponent } from 'echarts/components';

  echarts.use([GaugeChart, CanvasRenderer, TitleComponent, TooltipComponent]);

  let {
    percent = 0,
    label = 'Budget Used',
  }: {
    percent: number;
    label?: string;
  } = $props();

  let chartEl: HTMLDivElement | undefined = $state();
  let chart: echarts.ECharts | null = null;

  function buildOption(pct: number): echarts.EChartsCoreOption {
    return {
      backgroundColor: 'transparent',
      series: [
        {
          type: 'gauge',
          startAngle: 180,
          endAngle: 0,
          radius: '90%',
          center: ['50%', '70%'],
          min: 0,
          max: 100,
          splitNumber: 5,
          axisLine: {
            lineStyle: {
              width: 16,
              color: [
                [0.6, '#22c55e'],
                [0.8, '#eab308'],
                [1, '#ef4444'],
              ],
            },
          },
          pointer: {
            icon: 'path://M12.8,0.7l12,40.1H0.7L12.8,0.7z',
            length: '60%',
            width: 8,
            offsetCenter: [0, '-50%'],
            itemStyle: { color: 'oklch(0.65 0.19 30)' },
          },
          axisTick: { distance: -20, length: 6, lineStyle: { color: '#fff', width: 1 } },
          splitLine: { distance: -24, length: 16, lineStyle: { color: '#fff', width: 2 } },
          axisLabel: { color: 'oklch(0.70 0.01 80)', fontSize: 10, distance: -40 },
          title: { offsetCenter: [0, '20%'], fontSize: 12, color: 'oklch(0.70 0.01 80)' },
          detail: {
            fontSize: 24,
            fontWeight: 'bold',
            offsetCenter: [0, '-10%'],
            valueAnimation: true,
            formatter: (v: number) => `${v.toFixed(1)}%`,
            color: 'oklch(0.90 0.01 80)',
          },
          data: [{ value: pct, name: label }],
        },
      ],
    };
  }

  $effect(() => {
    if (!chartEl) return;

    chart = echarts.init(chartEl, 'dark', { renderer: 'canvas' });
    chart.setOption(buildOption(percent));

    const resizeObserver = new ResizeObserver(() => chart?.resize());
    resizeObserver.observe(chartEl);

    return () => {
      resizeObserver.disconnect();
      chart?.dispose();
      chart = null;
    };
  });

  $effect(() => {
    chart?.setOption(buildOption(percent));
  });
</script>

<div bind:this={chartEl} class="w-full h-48"></div>
