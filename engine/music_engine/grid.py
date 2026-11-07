"""The integer step grid. Every position is (bar, step); ticks and beats are derived here and nowhere else."""
from dataclasses import dataclass, asdict

PPQ = 960  # ticks per quarter note in everything we write


@dataclass(frozen=True)
class Grid:
    bars: int
    beats_per_bar: int = 4
    steps_per_beat: int = 4
    ppq: int = PPQ

    def __post_init__(self):
        if self.bars < 1:
            raise ValueError('bars must be at least 1')
        if self.ppq % self.steps_per_beat:
            raise ValueError(f'ppq {self.ppq} is not divisible by steps_per_beat {self.steps_per_beat}')

    @property
    def steps_per_bar(self):
        return self.beats_per_bar * self.steps_per_beat

    @property
    def total_steps(self):
        return self.bars * self.steps_per_bar

    @property
    def step_ticks(self):
        return self.ppq // self.steps_per_beat

    @property
    def end_tick(self):
        return self.total_steps * self.step_ticks

    @property
    def length_beats(self):
        return self.bars * self.beats_per_bar

    def positions(self):
        """Every (bar, step) in time order."""
        for bar in range(self.bars):
            for step in range(self.steps_per_bar):
                yield bar, step

    def tick(self, bar, step):
        return (bar * self.steps_per_bar + step) * self.step_ticks


@dataclass
class Note:
    """One rendered note. Position is on the grid; `offset` is the groove's nudge in ticks."""
    part: str
    lane: str
    bar: int
    step: int
    pitch: int
    velocity: int
    duration: int  # ticks
    offset: int = 0  # ticks, from the groove layer

    def start(self, grid):
        return grid.tick(self.bar, self.step) + self.offset

    def to_dict(self, grid):
        d = asdict(self)
        start = self.start(grid)
        d['tick'] = start
        d['beat'] = round(start / grid.ppq, 6)
        d['duration_beats'] = round(self.duration / grid.ppq, 6)
        return d
