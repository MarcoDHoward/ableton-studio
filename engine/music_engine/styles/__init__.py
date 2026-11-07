"""Style packs: TOML data files in this folder."""
import os
import tomllib

HERE = os.path.dirname(os.path.abspath(__file__))


def available():
    return sorted(f[:-5] for f in os.listdir(HERE) if f.endswith('.toml'))


def load(name_or_path):
    """Load a style by name ('techno_peak') or by path to a .toml file."""
    path = name_or_path
    if not os.path.isfile(path):
        path = os.path.join(HERE, name_or_path + '.toml')
    if not os.path.isfile(path):
        raise ValueError(f'unknown style {name_or_path!r} (have: {", ".join(available())})')
    with open(path, 'rb') as fh:
        return tomllib.load(fh)
