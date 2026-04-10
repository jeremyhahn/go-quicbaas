<!--
  DAGGraph renders an ECharts graph visualization of job work items as nodes
  with dependency edges. Nodes are colored by status/priority.
-->
<script lang="ts">
  import * as echarts from 'echarts/core';
  import { GraphChart } from 'echarts/charts';
  import { CanvasRenderer } from 'echarts/renderers';
  import {
    TitleComponent,
    TooltipComponent,
    LegendComponent,
  } from 'echarts/components';
  import type { WorkItem } from '../../stores/orchestrator.svelte.ts';

  echarts.use([GraphChart, CanvasRenderer, TitleComponent, TooltipComponent, LegendComponent]);

  let { workItems = [] }: { workItems: WorkItem[] } = $props();

  let chartEl: HTMLDivElement | undefined = $state();
  let chart: echarts.ECharts | null = null;

  function buildOption(items: WorkItem[]): echarts.EChartsCoreOption {
    const nodes = items.map((item) => ({
      id: item.id,
      name: item.title || item.id,
      value: item.priority,
      symbolSize: Math.max(20, Math.min(50, 20 + item.priority * 5)),
      itemStyle: {
        color: item.parallelizable ? 'oklch(0.65 0.19 30)' : 'oklch(0.58 0.22 25)',
      },
      label: { show: true, fontSize: 10 },
      tooltip: { formatter: item.description || item.title },
    }));

    const edges: Array<{ source: string; target: string }> = [];
    for (const item of items) {
      for (const dep of item.dependencies ?? []) {
        edges.push({ source: dep, target: item.id });
      }
    }

    return {
      backgroundColor: 'transparent',
      tooltip: { trigger: 'item' },
      series: [
        {
          type: 'graph',
          layout: 'force',
          roam: true,
          draggable: true,
          data: nodes,
          edges,
          force: { repulsion: 120, edgeLength: 80 },
          lineStyle: { color: 'oklch(0.40 0.02 30)', curveness: 0.1 },
          edgeSymbol: ['none', 'arrow'],
          edgeSymbolSize: [0, 8],
          label: { position: 'right', color: 'oklch(0.90 0.01 80)' },
          emphasis: { focus: 'adjacency' },
        },
      ],
    };
  }

  $effect(() => {
    if (!chartEl) return;

    chart = echarts.init(chartEl, 'dark', { renderer: 'canvas' });
    chart.setOption(buildOption(workItems));

    const resizeObserver = new ResizeObserver(() => chart?.resize());
    resizeObserver.observe(chartEl);

    return () => {
      resizeObserver.disconnect();
      chart?.dispose();
      chart = null;
    };
  });

  $effect(() => {
    if (chart) {
      chart.setOption(buildOption(workItems));
    }
  });
</script>

<div bind:this={chartEl} class="w-full h-80 rounded-box"></div>
