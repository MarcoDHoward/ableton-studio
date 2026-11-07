"""The groove layer: timing nudges applied after the notes are chosen, never baked into the grid.

Swing uses the MPC meaning: the percentage says where the second note of each
pair lands inside the pair. 50 = straight, 66 = triplet feel, 75 = dotted.
`swing_grid` picks the pair size: 16 swings 16ths (pairs of 16ths make an 8th),
8 swings 8ths. Positions inside a pair are warped proportionally, so a 16th
that sits between swung 8ths moves by a matching amount.
"""
from dataclasses import dataclass


@dataclass(frozen=True)
class Groove:
    swing: float = 50.0
    swing_grid: int = 16
    amount: float = 1.0  # Live's Groove Pool "Amount": 0 = no groove, 1 = full

    def __post_init__(self):
        if not 50 <= self.swing <= 75:
            raise ValueError(f'swing {self.swing} is outside the MPC range 50-75')
        if self.swing_grid not in (8, 16):
            raise ValueError('swing_grid must be 8 or 16')
        if not 0 <= self.amount <= 1:
            raise ValueError('amount must be between 0 and 1')

    def scaled(self, amount):
        return Groove(self.swing, self.swing_grid, self.amount * amount)

    def offset(self, step, grid):
        """Ticks to delay grid step `step` (within its bar)."""
        if self.swing == 50 or self.amount == 0:
            return 0
        unit = grid.ppq * 4 // self.swing_grid  # ticks in one swing unit (a 16th or an 8th)
        t = (step * grid.step_ticks) % (2 * unit)  # position inside the pair
        s = 2 * unit * self.swing / 100  # where the pair's second unit lands
        warped = t * s / unit if t <= unit else s + (t - unit) * (2 * unit - s) / unit
        return round((warped - t) * self.amount)
