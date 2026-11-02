import { dnfSatisfied } from "@shared/mod-viewer/eval";
import type { ViewerShapeWave, ViewerStateValue } from "@shared/mod-viewer/types";

/**
 * Advances the phases of wave shape targets as the mod's accumulators do. They grow
 * only while their wave's condition holds, at the speed the state sets then, so a
 * phase depends on its history: the clip clock only tells how much time has passed,
 * and a clip restarting does not rewind them. Scrubbing back runs them back.
 */
export class ShapeWavePlayback {
    private readonly waves = new Map<string, ViewerShapeWave>();
    private readonly phases = new Map<string, number>();
    private previousClip?: string;
    private previousTime?: number;

    constructor(targets: Iterable<{ var: string; wave?: ViewerShapeWave }>) {
        for (const target of targets) {
            if (target.wave && !this.waves.has(target.var)) {
                this.waves.set(target.var, target.wave);
            }
        }
    }

    get empty(): boolean {
        return this.waves.size === 0;
    }

    advance(
        clip: string,
        time: number,
        duration: number,
        state: Record<string, ViewerStateValue>,
    ): Record<string, number> {
        let elapsed =
            this.previousTime === undefined || clip !== this.previousClip
                ? 0
                : time - this.previousTime;
        if (elapsed < -duration / 2) {
            elapsed += duration;
        } else if (elapsed > duration / 2) {
            elapsed -= duration;
        }
        this.previousClip = clip;
        this.previousTime = time;

        const values: Record<string, number> = {};
        for (const [phaseVar, wave] of this.waves) {
            let phase = this.phases.get(phaseVar) ?? (Number(state[phaseVar]) || 0);
            if (dnfSatisfied(wave.when, state)) {
                const speed = wave.speedVar ? Number(state[wave.speedVar]) || 0 : 1;
                phase += elapsed * wave.rate * speed;
            }
            this.phases.set(phaseVar, phase);
            values[phaseVar] = phase;
        }
        return values;
    }
}
