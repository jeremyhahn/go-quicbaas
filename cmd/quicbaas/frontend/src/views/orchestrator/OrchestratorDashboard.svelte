<!--
  OrchestratorDashboard shows summary stats, job-state doughnut, agent
  status bar chart, and the 10 most recent events.
-->
<script lang="ts">
  import * as echarts from 'echarts/core';
  import { PieChart, BarChart } from 'echarts/charts';
  import { CanvasRenderer } from 'echarts/renderers';
  import {
    TitleComponent,
    TooltipComponent,
    LegendComponent,
    GridComponent,
  } from 'echarts/components';
  import { orchestratorStore } from '../../stores/orchestrator.svelte.ts';
  import StatCard from '../../components/core/StatCard.svelte';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import EventLog from '../../components/orchestrator/EventLog.svelte';
  import {
    Workflow,
    Bot,
    Terminal,
    ClipboardList,
  } from 'lucide-svelte';

  echarts.use([PieChart, BarChart, CanvasRenderer, TitleComponent, TooltipComponent, LegendComponent, GridComponent]);

  // ---------------------------------------------------------------------------
  // Derived stats
  // ---------------------------------------------------------------------------

  const activeJobs = $derived(
    orchestratorStore.jobs.filter((j) => j.state !== 'completed' && j.state !== 'failed').length
  );

  const pendingTasks = $derived(
    orchestratorStore.tasks.filter((t) => t.status === 'pending').length
  );

  const recentEvents = $derived(orchestratorStore.events.slice(-10));

  // ---------------------------------------------------------------------------
  // Job state doughnut chart
  // ---------------------------------------------------------------------------

  let pieEl: HTMLDivElement | undefined = $state();
  let pieChart: echarts.ECharts | null = null;

  function buildPieOption(): echarts.EChartsCoreOption {
    const counts: Record<string, number> = {};
    for (const job of orchestratorStore.jobs) {
      counts[job.state] = (counts[job.state] ?? 0) + 1;
    }

    return {
      backgroundColor: 'transparent',
      tooltip: { trigger: 'item' },
      legend: { orient: 'vertical', right: 10, textStyle: { color: 'oklch(0.70 0.01 80)', fontSize: 11 } },
      series: [
        {
          type: 'pie',
          radius: ['40%', '70%'],
          center: ['40%', '50%'],
          avoidLabelOverlap: false,
          label: { show: false },
          emphasis: { label: { show: true, fontSize: 14, fontWeight: 'bold' } },
          data: Object.entries(counts).map(([name, value]) => ({ name, value })),
        },
      ],
    };
  }

  $effect(() => {
    if (!pieEl) return;
    pieChart = echarts.init(pieEl, 'dark', { renderer: 'canvas' });
    pieChart.setOption(buildPieOption());
    const ro = new ResizeObserver(() => pieChart?.resize());
    ro.observe(pieEl);
    return () => { ro.disconnect(); pieChart?.dispose(); pieChart = null; };
  });

  $effect(() => { pieChart?.setOption(buildPieOption()); });

  // ---------------------------------------------------------------------------
  // Agent status bar chart
  // ---------------------------------------------------------------------------

  let barEl: HTMLDivElement | undefined = $state();
  let barChart: echarts.ECharts | null = null;

  function buildBarOption(): echarts.EChartsCoreOption {
    const statuses = ['registered', 'running', 'idle', 'offline', 'error'];
    const values = statuses.map(
      (s) => orchestratorStore.agents.filter((a) => a.status === s).length
    );

    return {
      backgroundColor: 'transparent',
      tooltip: { trigger: 'axis' },
      grid: { left: '10%', right: '5%', top: '10%', bottom: '15%' },
      xAxis: {
        type: 'category',
        data: statuses,
        axisLabel: { color: 'oklch(0.70 0.01 80)', fontSize: 11 },
        axisLine: { lineStyle: { color: 'oklch(0.25 0.02 30)' } },
      },
      yAxis: {
        type: 'value',
        axisLabel: { color: 'oklch(0.70 0.01 80)', fontSize: 11 },
        splitLine: { lineStyle: { color: 'oklch(0.18 0.02 30)' } },
      },
      series: [
        {
          type: 'bar',
          data: values,
          itemStyle: {
            color: (params: { dataIndex: number }) => {
              const colors = ['#60a5fa', '#22c55e', '#94a3b8', '#6b7280', '#ef4444'];
              return colors[params.dataIndex] ?? '#ea580c';
            },
          },
          barMaxWidth: 40,
        },
      ],
    };
  }

  $effect(() => {
    if (!barEl) return;
    barChart = echarts.init(barEl, 'dark', { renderer: 'canvas' });
    barChart.setOption(buildBarOption());
    const ro = new ResizeObserver(() => barChart?.resize());
    ro.observe(barEl);
    return () => { ro.disconnect(); barChart?.dispose(); barChart = null; };
  });

  $effect(() => { barChart?.setOption(buildBarOption()); });
</script>

<div class="p-6 space-y-6">
  <PageHeader title="Orchestration Dashboard" subtitle="Real-time overview of jobs, agents, and tasks" />

  <!-- Stat cards -->
  <div class="grid grid-cols-2 lg:grid-cols-4 gap-4">
    <StatCard
      label="Active Jobs"
      value={activeJobs}
      description="{orchestratorStore.jobs.length} total"
      icon={Workflow}
      variant={activeJobs > 0 ? 'primary' : 'default'}
    />
    <StatCard
      label="Agents"
      value={orchestratorStore.agents.length}
      description="{orchestratorStore.agents.filter((a) => a.status === 'running').length} running"
      icon={Bot}
    />
    <StatCard
      label="Sessions"
      value={orchestratorStore.sessions.length}
      description="active sessions"
      icon={Terminal}
    />
    <StatCard
      label="Pending Tasks"
      value={pendingTasks}
      description="{orchestratorStore.tasks.length} total"
      icon={ClipboardList}
      variant={pendingTasks > 0 ? 'warning' : 'default'}
    />
  </div>

  <!-- Charts row -->
  <div class="grid grid-cols-1 lg:grid-cols-2 gap-4">
    <div class="card bg-base-200 rounded-box">
      <div class="card-body p-4">
        <h3 class="text-sm font-semibold text-base-content/70 mb-2">Job States</h3>
        {#if orchestratorStore.jobs.length === 0}
          <div class="flex items-center justify-center h-48 text-base-content/30 text-sm">
            No jobs
          </div>
        {:else}
          <div bind:this={pieEl} class="w-full h-48"></div>
        {/if}
      </div>
    </div>

    <div class="card bg-base-200 rounded-box">
      <div class="card-body p-4">
        <h3 class="text-sm font-semibold text-base-content/70 mb-2">Agent Status Breakdown</h3>
        {#if orchestratorStore.agents.length === 0}
          <div class="flex items-center justify-center h-48 text-base-content/30 text-sm">
            No agents registered
          </div>
        {:else}
          <div bind:this={barEl} class="w-full h-48"></div>
        {/if}
      </div>
    </div>
  </div>

  <!-- Recent events -->
  <div class="card bg-base-200 rounded-box">
    <div class="card-body p-4">
      <h3 class="text-sm font-semibold text-base-content/70 mb-2">Recent Events</h3>
      <EventLog events={recentEvents} maxHeight="200px" autoScroll={false} />
    </div>
  </div>
</div>
