import type { RunState, RunStep } from '../types/domain';

export interface RunStoreState {
  runs: RunState[];
  stepsByRun: Record<string, RunStep[]>;
}

export function upsertRun(state: RunStoreState, run: RunState): RunStoreState {
  const exists = state.runs.some((item) => item.run_id === run.run_id);
  return {
    ...state,
    runs: exists ? state.runs.map((item) => item.run_id === run.run_id ? run : item) : [run, ...state.runs]
  };
}

export function setRunSteps(state: RunStoreState, runId: string, steps: RunStep[]): RunStoreState {
  return {
    ...state,
    stepsByRun: {
      ...state.stepsByRun,
      [runId]: steps
    }
  };
}

