<!--
  JobStreamHarness holds a job's event stream open the way JobDetailView
  does: one $effect that pins the job while it is set and releases it in the
  effect's cleanup. It exists so a test can drive the REAL job-events store
  through the same reactive shape the view uses, which is where the
  effect_update_depth_exceeded loop lived.
-->
<script lang="ts">
  import { startEventStream, stopEventStream } from '../../src/stores/job-events.svelte.ts';

  let { tenantID, jobID }: { tenantID: string; jobID: string } = $props();

  $effect(() => {
    const id = jobID;
    const tenant = tenantID;
    if (id === '') {
      stopEventStream();
      return;
    }
    startEventStream(tenant, id);
    return () => { stopEventStream(); };
  });
</script>
