"""Read-only extractor for an Ableton Live 12 .als set.

Writes set.json: global info, and per track its mixer, device tree (with parameter
values and plugin identities), session + arrangement clips (MIDI notes, audio file
refs, clip envelopes) and track automation resolved to parameter names.

usage: python extract_als.py <set.als> <out.json>
"""
import gzip, json, sys
import xml.etree.ElementTree as ET

def val(el, path, cast=str, default=None):
    if el is None: return default
    e = el.find(path)
    if e is None or e.get('Value') is None: return default
    v = e.get('Value')
    try:
        if cast is bool: return v == 'true'
        return cast(v)
    except ValueError:
        return v

DEVICE_CONTAINERS = {'Devices'}

def device_label(d):
    tag = d.tag
    if tag == 'PluginDevice':
        info = d.find('PluginDesc')
        if info is not None and len(info):
            pi = info[0]
            name = val(pi, 'Name') or val(pi, 'PlugName')
            vendor = val(pi, 'Manufacturer')
            return name or 'Plugin', {'format': pi.tag, 'vendor': vendor}
        return 'Plugin', {}
    if tag.startswith('MxDevice'):
        p = d.find('.//PatchSlot//FileRef/RelativePath')
        if p is None: p = d.find('.//FileRef/Path')
        path = p.get('Value') if p is not None else None
        return (path.replace('\\', '/').split('/')[-1] if path else tag), {'max_patch': path}
    return tag, {}

def params_of(d, idmap, owner):
    """Native params: direct-ish children holding <Manual Value=..> plus an AutomationTarget."""
    out = {}
    for child in d:
        if child.tag in ('Branches', 'DeviceChain', 'SourceContext', 'Pointee', 'LastPresetRef'):
            continue
        man, at = child.find('Manual'), child.find('AutomationTarget')
        if man is not None and man.get('Value') is not None:
            v = man.get('Value')
            try: v = float(v)
            except ValueError: v = (v == 'true') if v in ('true', 'false') else v
            out[child.tag] = v
            if at is not None: idmap[at.get('Id')] = (owner, child.tag)
        elif len(child) and child.tag not in ('Devices',):
            # one level of nesting (e.g. Operator <Operator.0><Envelope>..)
            for g in child:
                man, at = g.find('Manual'), g.find('AutomationTarget')
                if man is not None and man.get('Value') is not None:
                    v = man.get('Value')
                    try: v = float(v)
                    except ValueError: v = (v == 'true') if v in ('true', 'false') else v
                    out[child.tag + '.' + g.tag] = v
                    if at is not None: idmap[at.get('Id')] = (owner, child.tag + '.' + g.tag)
    return out

def plugin_params(d, idmap, owner):
    out = {}
    for p in d.iter('PluginFloatParameter'):
        name = val(p, 'ParameterName') or ('param%s' % val(p, 'ParameterId'))
        pv = p.find('ParameterValue')
        out[name] = val(pv, 'Manual', float)
        at = pv.find('AutomationTarget') if pv is not None else None
        if at is not None: idmap[at.get('Id')] = (owner, name)
    return out

def sample_ref(el):
    fr = el.find('.//SampleRef/FileRef')
    if fr is None: return None
    return val(fr, 'Path') or val(fr, 'RelativePath')

def walk_devices(devs_el, idmap, prefix):
    out = []
    if devs_el is None: return out
    for i, d in enumerate(devs_el):
        label, extra = device_label(d)
        user = val(d, 'UserName') or ''
        owner = '%s/%d:%s' % (prefix, i, user or label)  # index disambiguates repeated devices
        dev = {'type': d.tag, 'name': user or label, 'on': val(d, 'On/Manual', bool, True), **extra}
        if d.tag == 'PluginDevice':
            dev['params'] = plugin_params(d, idmap, owner)
            prog = d.find('.//PluginDesc//Preset')
            if prog is not None:
                pn = prog.find('.//Name')
                if pn is None: pn = prog.find('.//ProgramName')
                if pn is not None: dev['preset'] = pn.get('Value')
        else:
            dev['params'] = params_of(d, idmap, owner)
        if d.tag in ('OriginalSimpler', 'MultiSampler'):
            dev['samples'] = sorted({p.get('Value') for p in d.iter('Path') if p.get('Value')})
        br = d.find('Branches')
        if br is not None:
            dev['chains'] = []
            for b in br:
                ch = {'name': val(b, 'Name/EffectiveName') or val(b, 'Name') or b.tag}
                note = val(b, 'BranchInfo/ReceivingNote', int)
                if note is not None: note = 128 - note  # Live stores pads as 128 - MIDI note
                if note is not None: ch['note'] = note
                inner = b.find('.//Devices')
                ch['devices'] = walk_devices(inner, idmap, owner + '/' + ch['name'])
                dev['chains'].append(ch)
        out.append(dev)
    return out

def midi_notes(clip):
    notes = []
    for kt in clip.iter('KeyTrack'):
        key = val(kt, 'MidiKey', int)
        for n in kt.iter('MidiNoteEvent'):
            if n.get('IsEnabled', 'true') != 'true': continue
            notes.append([key, float(n.get('Time')), float(n.get('Duration')),
                          float(n.get('Velocity'))])
    notes.sort(key=lambda x: (x[1], x[0]))
    return notes

def events(env_el):
    evs = []
    ev = env_el.find('Automation/Events')
    if ev is None: return evs
    for e in ev:
        t, v = e.get('Time'), e.get('Value')
        try: evs.append([float(t), float(v) if v not in ('true', 'false') else float(v == 'true')])
        except (TypeError, ValueError): pass
    return evs

def clip_info(c, idmap_pending):
    info = {
        'kind': 'midi' if c.tag == 'MidiClip' else 'audio',
        'name': val(c, 'Name') or '',
        'time': float(c.get('Time', 0)),
        'start': val(c, 'CurrentStart', float), 'end': val(c, 'CurrentEnd', float),
        'loop': {'on': val(c, 'Loop/LoopOn', bool), 'start': val(c, 'Loop/LoopStart', float),
                 'end': val(c, 'Loop/LoopEnd', float)},
        'disabled': val(c, 'Disabled', bool, False),
    }
    if c.tag == 'MidiClip':
        info['notes'] = midi_notes(c)
    else:
        info['file'] = sample_ref(c)
        info['warp'] = val(c, 'IsWarped', bool)
        info['pitch'] = val(c, 'PitchCoarse', float)
        info['gain'] = val(c, 'SampleVolume', float)
    envs = []
    for ce in c.iter('ClipEnvelope'):
        envs.append({'pointee': val(ce, 'EnvelopeTarget/PointeeId'), 'events': events(ce)})
    if envs:
        info['envelopes'] = envs
        idmap_pending.append(info)
    return info

def mixer(t, idmap, owner):
    m = t.find('DeviceChain/Mixer')
    if m is None: return {}
    out = {'volume': val(m, 'Volume/Manual', float), 'pan': val(m, 'Pan/Manual', float),
           'speaker_on': val(m, 'Speaker/Manual', bool, True)}
    for tag in ('Volume', 'Pan', 'Speaker', 'Tempo', 'TimeSignature', 'CrossFadeState'):
        at = m.find(tag + '/AutomationTarget')
        if at is not None: idmap[at.get('Id')] = (owner + '/Mixer', tag)
    sends = []
    for i, s in enumerate(m.iter('TrackSendHolder')):
        sends.append(val(s, 'Send/Manual', float))
        at = s.find('Send/AutomationTarget')
        if at is not None: idmap[at.get('Id')] = (owner + '/Mixer', 'Send%s' % 'ABCDEFGH'[i])
    out['sends'] = sends
    return out

def track_info(t, idmap, pending):
    name = val(t, 'Name/EffectiveName')
    tr = {'id': t.get('Id'), 'type': t.tag, 'name': name, 'group': val(t, 'TrackGroupId'),
          'color': val(t, 'Color', int)}
    tr['mixer'] = mixer(t, idmap, name)
    tr['devices'] = walk_devices(t.find('DeviceChain/DeviceChain/Devices'), idmap, name)
    seq = t.find('DeviceChain/MainSequencer')
    session, arrangement = [], []
    if seq is not None:
        for i, cs in enumerate(seq.findall('ClipSlotList/ClipSlot')):
            for c in cs.iter():
                if c.tag in ('MidiClip', 'AudioClip'):
                    ci = clip_info(c, pending); ci['slot'] = i; session.append(ci)
        for tag in ('Sample', 'ClipTimeable'):
            ev = seq.find(tag + '/ArrangerAutomation/Events')
            if ev is None: continue
            for c in ev:
                if c.tag in ('MidiClip', 'AudioClip'):
                    arrangement.append(clip_info(c, pending))
    tr['session_clips'], tr['arrangement_clips'] = session, arrangement
    tr['automation'] = [{'pointee': val(e, 'EnvelopeTarget/PointeeId'), 'events': events(e)}
                        for e in t.findall('AutomationEnvelopes/Envelopes/AutomationEnvelope')]
    return tr

def main(src, dst):
    root = ET.fromstring(gzip.open(src).read())
    ls = root.find('LiveSet')
    idmap, pending = {}, []
    tracks = [track_info(t, idmap, pending) for t in ls.find('Tracks')]
    returns_ = []
    main_t = ls.find('MainTrack')
    if main_t is None: main_t = ls.find('MasterTrack')
    master = track_info(main_t, idmap, pending) if main_t is not None else None
    def resolve(envs):
        for e in envs:
            owner, param = idmap.get(e['pointee'], ('?', '?'))
            e['owner'], e['param'] = owner, param
    for t in tracks + ([master] if master else []):
        resolve(t['automation'])
    for c in pending:
        resolve(c['envelopes'])
    tempo = val(main_t, 'DeviceChain/Mixer/Tempo/Manual', float)
    out = {
        'source': src, 'creator': root.get('Creator'), 'tempo': tempo,
        'time_signature': val(main_t, 'DeviceChain/Mixer/TimeSignature/Manual', int),
        'scale': {'root': val(ls, 'ScaleInformation/Root', int) if ls.find('ScaleInformation/Root') is not None
                  else val(ls, 'ScaleInformation/RootNote', int),
                  'name': val(ls, 'ScaleInformation/Name')},
        'locators': [{'time': val(l, 'Time', float), 'name': val(l, 'Name')}
                     for l in ls.findall('Locators/Locators/Locator')],
        'scenes': [val(s, 'Name') for s in ls.findall('Scenes/Scene')],
        'tracks': tracks, 'master': master,
    }
    json.dump(out, open(dst, 'w', encoding='utf-8'), indent=1)
    return out

if __name__ == '__main__':
    o = main(sys.argv[1], sys.argv[2])
    print('tempo', o['tempo'], 'scale', o['scale'], 'locators', o['locators'])
    for t in o['tracks'] + [o['master']]:
        notes = sum(len(c.get('notes', [])) for c in t['session_clips'] + t['arrangement_clips'])
        print('%-26s %-11s sess=%-2d arr=%-3d notes=%-4d auto=%-2d devs=%s' % (
            t['name'][:26], t['type'], len(t['session_clips']), len(t['arrangement_clips']), notes,
            len(t['automation']), ','.join(d['name'] for d in t['devices'])[:90]))
