"""
Axiom-style space cruise liner, built headless with bpy.
Stages (argv after "--"): build [render N] [bake] [export]
Blender: +X bow, +Z up, +Y port. glTF export converts to +Y up.
"""
import bpy, bmesh, math, sys, os, random
from mathutils import Vector, Matrix
from mathutils.bvhtree import BVHTree

WORK = os.path.dirname(os.path.abspath(__file__))
ARGS = sys.argv[sys.argv.index("--") + 1:] if "--" in sys.argv else []

# ------------------------------------------------------------------ profile (ported from hullProfile.ts)
def cubic(s, t):
    u = 1 - t
    a, b, c, d = u*u*u, 3*u*u*t, 3*u*t*t, t*t*t
    return (a*s[0][0] + b*s[1][0] + c*s[2][0] + d*s[3][0], a*s[0][1] + b*s[1][1] + c*s[2][1] + d*s[3][1])

def sample(segs, n=96):
    out = []
    for s in segs:
        for i in range(n + 1):
            out.append(cubic(s, i / n))
    return out

def interp(poly, v, axis):
    o = 1 - axis
    for i in range(1, len(poly)):
        a, b = poly[i - 1], poly[i]
        lo, hi = min(a[axis], b[axis]), max(a[axis], b[axis])
        if lo <= v <= hi and hi > lo:
            return a[o] + (v - a[axis]) / (b[axis] - a[axis]) * (b[o] - a[o])
    return None

STERN_TOP, STERN_BOTTOM = (110, 246), (110, 314)
HULL_TOP = [[STERN_TOP, (400, 242), (720, 214), (886, 200)]]
BOW = [[(886, 200), (944, 200), (982, 228), (982, 264)], [(982, 264), (982, 300), (944, 332), (886, 334)]]
BELLY = [[(886, 334), (620, 352), (300, 340), STERN_BOTTOM]]
SWEEP_START = (170, 243)
SWEEP = [[SWEEP_START, (330, 240), (540, 150), (730, 138)], [(730, 138), (800, 134), (860, 150), (886, 200)]]
SVG_STERN_X, SVG_NOSE_X, SVG_DOME = 110, 982, (752, 138)

HULL_TOP_POLY = sample(HULL_TOP)
SWEEP_POLY = sample(SWEEP)
BELLY_POLY = list(reversed(sample(BELLY)))
BOW_POLY = sample(BOW)
BOW_UPPER, BOW_LOWER = sample([BOW[0]]), sample([BOW[1]])
hullTopAt = lambda x: interp(HULL_TOP_POLY, x, 0)
sweepAt = lambda x: interp(SWEEP_POLY, x, 0)
bellyAt = lambda x: interp(BELLY_POLY, x, 0)
bowFrontAt = lambda y: interp(BOW_POLY, y, 1)

SHIP_LEN = 10.0
S = SHIP_LEN / (SVG_NOSE_X - SVG_STERN_X)
SVG_MID_X = (SVG_NOSE_X + SVG_STERN_X) / 2
SVG_AXIS_Y = 272
wx = lambda x: (x - SVG_MID_X) * S
wz = lambda y: (SVG_AXIS_Y - y) * S
wl = lambda l: l * S
K = SHIP_LEN / 6.2  # ts world units -> ours

def smooth01(t):
    x = min(1, max(0, t))
    return x * x * (3 - 2 * x)

class Ring:
    __slots__ = ("x", "c", "h", "w", "pu", "pl")
    def __init__(self, x, c, h, w, pu=1.0, pl=1.0):
        self.x, self.c, self.h, self.w, self.pu, self.pl = x, c, h, w, pu, pl

def aspect_at(svgX):
    u = min(1, max(0, (svgX - SVG_STERN_X) / (SVG_NOSE_X - SVG_STERN_X)))
    return 1.25 + 0.8 * (1 - u) ** 1.5

def exps_at(svgX):
    u = min(1, max(0, (svgX - SVG_STERN_X) / (SVG_NOSE_X - SVG_STERN_X)))
    return 1.0 - 0.12 * (1 - u), 0.96 - 0.22 * (1 - u)   # upper, lower

def section_at(svgX):
    if svgX < SVG_STERN_X or svgX > SVG_NOSE_X:
        return None
    if svgX <= 886:
        xx = max(svgX, SVG_STERN_X + 0.01)
        return hullTopAt(xx), bellyAt(xx)
    top, bot = interp(BOW_UPPER, svgX, 0), interp(BOW_LOWER, svgX, 0)
    if top is None or bot is None:
        return 264, 264
    return top, bot

def hull_ring(svgX):
    s = section_at(svgX)
    if s is None:
        return None
    top, bot = s
    h = (bot - top) / 2 * S
    pu, pl = exps_at(svgX)
    return Ring(wx(svgX), wz((top + bot) / 2), h, h * aspect_at(svgX), pu, pl)

def ring_pt(r, theta):
    """theta measured from top, positive toward +Y."""
    ct, st = math.cos(theta), math.sin(theta)
    p = r.pu if ct >= 0 else r.pl
    z = r.c + r.h * math.copysign(abs(ct) ** p, ct)
    y = r.w * math.copysign(abs(st) ** p, st)
    return Vector((r.x, y, z))

def ring_normal2d(r, theta, eps=1e-3):
    a, b = ring_pt(r, theta + eps), ring_pt(r, theta - eps)
    t = Vector((a.y - b.y, a.z - b.z))
    n = Vector((t.y, -t.x))
    p = ring_pt(r, theta)
    if n.dot(Vector((p.y, p.z - r.c))) < 0:
        n = -n
    if n.length < 1e-9:
        return Vector((0, 0, 1))
    n.normalize()
    return Vector((0, n.x, n.y))

STERN_R = hull_ring(SVG_STERN_X + 0.01)
CAP_LEN = wl(26)
CAP_S0 = 0.72
X_STERN_END = STERN_R.x - CAP_LEN * CAP_S0
CAP_F0 = math.sqrt(1 - CAP_S0 ** 2)

def hull_rings():
    rings = []
    for s in [CAP_S0, 0.62, 0.5, 0.36, 0.2, 0.08]:
        f = math.sqrt(1 - s * s)
        rings.append(Ring(STERN_R.x - CAP_LEN * s, STERN_R.c, STERN_R.h * f, STERN_R.w * f, STERN_R.pu, STERN_R.pl))
    n = 46
    for i in range(n):
        t = i / n
        svgX = SVG_STERN_X + (SVG_NOSE_X - SVG_STERN_X) * math.sin(t * math.pi / 2)
        r = hull_ring(min(svgX, SVG_NOSE_X))
        if r and r.h > 1e-4:
            rings.append(r)
    return rings

def super_ring(svgX):
    hull = hull_ring(svgX)
    ys = sweepAt(svgX)
    if not hull or ys is None:
        return None
    W = hull.w * 0.5
    # hull surface height at |y| = W (solve superellipse for upper half)
    q = 1 - (W / hull.w) ** (2 / hull.pu) if hull.w > 1e-6 else 0
    zs = hull.c + hull.h * max(q, 0) ** (hull.pu / 2)
    base = zs + 0.035 - 0.025
    top = wz(ys)
    inS, inB = smooth01((svgX - 170) / 70), smooth01((886 - svgX) / 46)
    taper = inS * inB
    H = max(0, top - base) * taper
    return Ring(hull.x, base, H, W * (0.35 + 0.65 * taper), 0.65, 0.65)

# ------------------------------------------------------------------ mesh helpers
def new_obj(name, bm, mat=None, smooth=True):
    me = bpy.data.meshes.new(name)
    bm.to_mesh(me)
    bm.free()
    me.update()
    ob = bpy.data.objects.new(name, me)
    bpy.context.scene.collection.objects.link(ob)
    if mat:
        me.materials.append(mat)
    if smooth:
        for p in me.polygons:
            p.use_smooth = True
    return ob

def loft(name, rings, closed=True, uv_rect=(0, 0, 1, 1), mat=None, cap_start=False, cap_end=False, pole_start=None, pole_end=None):
    """rings: list of lists of Vector (same count). Returns object. UV: u along rings (by arc length), v around."""
    bm = bmesh.new()
    uv_layer = bm.loops.layers.uv.new("UVMap")
    m = len(rings[0])
    # arc-length parameter
    dists = [0.0]
    for i in range(1, len(rings)):
        dists.append(dists[-1] + (rings[i][0] - rings[i - 1][0]).length + 1e-6)
    total = dists[-1]
    ux0, uy0, ux1, uy1 = uv_rect
    def uv(i, j):
        return (ux0 + (ux1 - ux0) * dists[i] / total, uy0 + (uy1 - uy0) * j / m)
    verts = [[bm.verts.new(p) for p in ring] for ring in rings]
    faces = []
    jn = m if closed else m - 1
    for i in range(len(rings) - 1):
        for j in range(jn):
            j2 = (j + 1) % m
            a, b, c, d = verts[i][j], verts[i][j2], verts[i + 1][j2], verts[i + 1][j]
            try:
                f = bm.faces.new((a, b, c, d))
            except ValueError:
                continue
            f.loops[0][uv_layer].uv = uv(i, j)
            f.loops[1][uv_layer].uv = uv(i, j + 1)
            f.loops[2][uv_layer].uv = uv(i + 1, j + 1)
            f.loops[3][uv_layer].uv = uv(i + 1, j)
            faces.append(f)
    def pole(ring_idx, pt, reverse):
        pv = bm.verts.new(pt)
        for j in range(m):
            j2 = (j + 1) % m
            tri = (verts[ring_idx][j2], verts[ring_idx][j], pv) if reverse else (verts[ring_idx][j], verts[ring_idx][j2], pv)
            try:
                f = bm.faces.new(tri)
            except ValueError:
                continue
            uv0 = uv(ring_idx, j); uv1 = uv(ring_idx, j + 1)
            f.loops[0][uv_layer].uv = uv1 if reverse else uv0
            f.loops[1][uv_layer].uv = uv0 if reverse else uv1
            f.loops[2][uv_layer].uv = ((uv0[0] + uv1[0]) / 2 + (-0.002 if ring_idx == 0 else 0.002), (uv0[1] + uv1[1]) / 2)
    if pole_start is not None:
        pole(0, pole_start, True)
    if pole_end is not None:
        pole(len(rings) - 1, pole_end, False)
    if cap_start:
        f = bm.faces.new(list(reversed(verts[0])))
        for l in f.loops: l[uv_layer].uv = uv(0, 0)
    if cap_end:
        f = bm.faces.new(verts[-1])
        for l in f.loops: l[uv_layer].uv = uv(len(rings) - 1, 0)
    bmesh.ops.recalc_face_normals(bm, faces=bm.faces[:])
    return new_obj(name, bm, mat)

def prim_cylinder(name, r0, r1, length, segs=24, mat=None, cap0=True, cap1=True, uv_rect=(0, 0, 1, 1)):
    """Along +X, from x=0 (r0) to x=length (r1), centered at origin in Y/Z."""
    rings = []
    for x, r in ((0, r0), (length, r1)):
        rings.append([Vector((x, r * math.sin(2 * math.pi * j / segs), r * math.cos(2 * math.pi * j / segs))) for j in range(segs)])
    return loft(name, rings, closed=True, mat=mat, cap_start=cap0, cap_end=cap1, uv_rect=uv_rect)

def prim_box(name, sx, sy, sz, mat=None):
    bm = bmesh.new()
    bmesh.ops.create_cube(bm, size=1.0)
    bmesh.ops.scale(bm, vec=(sx, sy, sz), verts=bm.verts)
    uv_layer = bm.loops.layers.uv.new("UVMap")
    for f in bm.faces:
        for i, l in enumerate(f.loops):
            l[uv_layer].uv = ((i in (1, 2)) * 1.0, (i in (2, 3)) * 1.0)
    return new_obj(name, bm, mat, smooth=False)

def prim_sphere(name, r, mat=None, segs=24, rings=12, cut_below=None):
    bm = bmesh.new()
    bmesh.ops.create_uvsphere(bm, u_segments=segs, v_segments=rings, radius=r)
    uv_layer = bm.loops.layers.uv.new("UVMap")
    for f in bm.faces:
        for l in f.loops:
            v = l.vert.co
            l[uv_layer].uv = ((math.atan2(v.y, v.x) / (2 * math.pi)) % 1.0, (v.z / r + 1) / 2)
    if cut_below is not None:
        geom = [v for v in bm.verts if v.co.z < cut_below]
        bmesh.ops.delete(bm, geom=geom, context='VERTS')
    return new_obj(name, bm, mat)

def prim_torus(name, R, r, segs=48, rsegs=10, mat=None, scale_y=1.0, scale_z=1.0):
    """Ring in the Y-Z plane (axis along X), elliptical via scale."""
    rings = []
    for j in range(rsegs):
        a = 2 * math.pi * j / rsegs
        pts = []
        for i in range(segs):
            t = 2 * math.pi * i / segs
            cy, cz = math.sin(t) * R * scale_y, math.cos(t) * R * scale_z
            ny, nz = math.sin(t) * scale_z, math.cos(t) * scale_y
            nl = math.hypot(ny, nz); ny, nz = ny / nl, nz / nl
            pts.append(Vector((r * math.sin(a), cy + ny * r * math.cos(a), cz + nz * r * math.cos(a))))
        rings.append(pts)
    rings.append(rings[0])
    return loft(name, rings, closed=True, mat=mat)

def set_xform(ob, loc=(0, 0, 0), rot=(0, 0, 0), scale=(1, 1, 1)):
    ob.location = loc
    ob.rotation_euler = rot
    ob.scale = scale

def add_bevel(ob, width=0.02, segments=2, angle=30):
    md = ob.modifiers.new("Bevel", 'BEVEL')
    md.width = width
    md.segments = segments
    md.limit_method = 'ANGLE'
    md.angle_limit = math.radians(angle)
    md.harden_normals = False
    return md

def add_subsurf(ob, levels=1, render=None):
    md = ob.modifiers.new("Subd", 'SUBSURF')
    md.levels = levels
    md.render_levels = render if render is not None else levels
    return md

def select_only(obs, active=None):
    bpy.ops.object.select_all(action='DESELECT')
    for o in obs:
        o.select_set(True)
    bpy.context.view_layer.objects.active = active or obs[0]

def apply_all(ob):
    select_only([ob])
    bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
    for md in list(ob.modifiers):
        bpy.ops.object.modifier_apply(modifier=md.name)

def smart_uv(ob, rect):
    select_only([ob])
    bpy.ops.object.mode_set(mode='EDIT')
    bpy.ops.mesh.select_all(action='SELECT')
    bpy.ops.uv.smart_project(angle_limit=math.radians(66), island_margin=0.02, correct_aspect=True, scale_to_bounds=True)
    bpy.ops.object.mode_set(mode='OBJECT')
    remap_uv(ob, rect)

def remap_uv(ob, rect):
    x0, y0, x1, y1 = rect
    uv = ob.data.uv_layers.active.data
    for l in uv:
        l.uv = (x0 + l.uv.x * (x1 - x0), y0 + l.uv.y * (y1 - y0))

def srgb(hexstr, alpha=1.0):
    h = hexstr.lstrip('#')
    out = []
    for i in (0, 2, 4):
        c = int(h[i:i + 2], 16) / 255
        out.append(c / 12.92 if c <= 0.04045 else ((c + 0.055) / 1.055) ** 2.4)
    return (*out, alpha)

def principled_tree(m):
    """Make sure a material has a Principled BSDF wired to an output; return the BSDF node."""
    m.use_nodes = True
    nt = m.node_tree
    if "Principled BSDF" not in nt.nodes:
        nt.nodes.clear()
        out = nt.nodes.new("ShaderNodeOutputMaterial")
        b = nt.nodes.new("ShaderNodeBsdfPrincipled")
        b.name = "Principled BSDF"
        nt.links.new(b.outputs["BSDF"], out.inputs["Surface"])
    return nt.nodes["Principled BSDF"]

# ------------------------------------------------------------------ materials
ACCENT = "#6f7cff"
def make_materials(render_boost=True):
    mats = {}
    def principled(name, base, rough=0.5, metal=0.0, emit=None, strength=1.0):
        m = bpy.data.materials.new(name)
        bsdf = principled_tree(m)
        bsdf.inputs["Base Color"].default_value = base
        bsdf.inputs["Roughness"].default_value = rough
        bsdf.inputs["Metallic"].default_value = metal
        if emit:
            bsdf.inputs["Emission Color"].default_value = emit
            bsdf.inputs["Emission Strength"].default_value = strength
        mats[name] = m
        return m
    principled("Hull", srgb("#d6dae1"), 0.55, 0.1)
    principled("Windows", srgb("#141a26"), 0.2, 0.0, srgb("#fff4e0"), 3.2 if render_boost else 1.0)
    principled("BowBand", srgb("#141a26"), 0.2, 0.0, srgb(ACCENT), 3.0 if render_boost else 1.0)
    principled("Engine", srgb("#141a26"), 0.3, 0.0, srgb(ACCENT), 12.0 if render_boost else 1.0)
    principled("Beacon", srgb("#141a26"), 0.3, 0.0, srgb("#fff1d6"), 14.0 if render_boost else 1.0)
    return mats

# ------------------------------------------------------------------ build
def build():
    bpy.ops.wm.read_factory_settings(use_empty=True)
    scene = bpy.context.scene
    mats = make_materials()
    HULL, WIN = mats["Hull"], mats["Windows"]
    hull_parts, misc = [], {}

    # --- main hull: lofted superellipse sections
    hr = hull_rings()
    SEG = 32
    rings = [[ring_pt(r, 2 * math.pi * j / SEG) for j in range(SEG)] for r in hr]
    hull = loft("HullBody", rings, closed=True, uv_rect=(0.004, 0.56, 0.996, 1.0), mat=HULL,
                pole_start=Vector((X_STERN_END - 0.02, 0, STERN_R.c)), pole_end=Vector((wx(SVG_NOSE_X), 0, wz(264))))
    add_subsurf(hull, 1)
    hull_parts.append(hull)
    misc["hull_base_rings"] = hr

    # --- deck ledge under the superstructure
    TH_D = math.radians(44)
    dk = []
    n = 44
    for i in range(n + 1):
        svgX = 150 + (888 - 150) * i / n
        r = hull_ring(svgX)
        taper = smooth01((888 - svgX) / 55) * 0.05 + 0.004
        row = []
        m = 10
        for j in range(m + 1):
            th = -TH_D + 2 * TH_D * j / m
            p = ring_pt(r, th); nrm = ring_normal2d(r, th)
            row.append(p + nrm * taper)
        rowin = [ring_pt(r, -TH_D) + ring_normal2d(r, -TH_D) * -0.06] + row + [ring_pt(r, TH_D) + ring_normal2d(r, TH_D) * -0.06]
        dk.append(rowin)
    deck = loft("Deck", dk, closed=False, uv_rect=(0.0, 0.42, 1.0, 0.55), mat=HULL, cap_start=True, cap_end=True)
    add_bevel(deck, 0.014, 2, 40)
    hull_parts.append(deck)

    # --- belt lines (deck ledges) along the hull flanks, between the window rows
    for bi, deg in enumerate((70.5, 87.5, 104.5)):
        for side in (1, -1):
            th = side * math.radians(deg)
            rows = []
            n = 40
            for i in range(n + 1):
                svgX = 128 + (884 - 128) * i / n
                r = hull_ring(svgX)
                fade = smooth01((svgX - 128) / 50) * smooth01((884 - svgX) / 50)
                lift = 0.019 * fade - 0.006
                hw = 0.018
                pts = []
                for dth, off in ((-hw / r.h, -0.03), (-hw / r.h, lift), (hw / r.h, lift), (hw / r.h, -0.03)):
                    pts.append(ring_pt(r, th + dth) + ring_normal2d(r, th + dth) * off)
                rows.append(pts)
            belt = loft(f"Belt{bi}{'L' if side > 0 else 'R'}", rows, closed=False, uv_rect=(0.0, 0.272 + 0.0045 * (bi * 2 + (side < 0)), 1.0, 0.276 + 0.0045 * (bi * 2 + (side < 0))), mat=HULL, cap_start=True, cap_end=True)
            hull_parts.append(belt)

    # --- superstructure: half superellipse tube riding on the deck
    sr = []
    n = 40
    for i in range(n + 1):
        svgX = 172 + (884 - 172) * i / n
        r = super_ring(svgX)
        if r:
            sr.append(r)
    m = 18
    srings = []
    for r in sr:
        row = []
        for j in range(m + 1):
            th = -math.pi / 2 + math.pi * j / m
            p = ring_pt(r, th)
            if j in (0, m):
                p.z -= 0.045
            row.append(p)
        srings.append(row)
    sup = loft("Superstructure", srings, closed=False, uv_rect=(0.0, 0.30, 1.0, 0.41), mat=HULL)
    add_subsurf(sup, 1)
    hull_parts.append(sup)
    misc["super_rings"] = sr

    # --- bridge dome + rim + mast on the crest
    dome_c = Vector((wx(SVG_DOME[0]), 0, wz(SVG_DOME[1]) - 0.06))
    dome = prim_sphere("Dome", wl(15), HULL, 32, 16, cut_below=-wl(15) * 0.35)
    dome.location = dome_c
    rim = prim_torus("DomeRim", wl(15) * 1.02, 0.022, 48, 10, HULL)
    rim.rotation_euler = (0, math.radians(90), 0)
    rim.location = dome_c + Vector((0, 0, -wl(15) * 0.3))
    hull_parts += [dome, rim]
    mast = prim_cylinder("Mast", 0.014, 0.010, 0.62, 12, HULL)
    mast.rotation_euler = (0, math.radians(-90), 0)
    mast.location = dome_c + Vector((0, 0, wl(15) * 0.8))
    hull_parts.append(mast)
    for k, (h, ln) in enumerate(((0.40, 0.22), (0.52, 0.14))):
        bar = prim_box(f"MastBar{k}", 0.012, ln, 0.012, HULL)
        bar.location = dome_c + Vector((0, 0, wl(15) * 0.8 + h))
        add_bevel(bar, 0.003, 1)
        hull_parts.append(bar)
    beacon = prim_sphere("MastBeacon", 0.028, mats["Beacon"], 16, 8)
    beacon.location = dome_c + Vector((0, 0, wl(15) * 0.8 + 0.63))
    misc.setdefault("emissive", []).append(beacon)
    # small sensor dome behind bridge
    sd = prim_sphere("SensorDome", 0.07, HULL, 20, 10, cut_below=0.0)
    sd.location = Vector((wx(600), 0, wz(sweepAt(600)) - 0.01))
    hull_parts.append(sd)

    # --- fins (two wide swept wings at the stern)
    xs = STERN_R.x
    outline = [(xs + 1.35 * K, 0), (xs - 0.2 * K, 1.38 * K), (xs - 0.32 * K, 1.46 * K), (xs - 0.5 * K, 1.4 * K), (xs - 0.3 * K, 1.1 * K), (xs + 0.28 * K, 0)]
    span = 1.46 * K
    zf = STERN_R.c - 0.04
    dih = math.tan(0.18)
    for side, nm in ((1, "FinL"), (-1, "FinR")):
        bm = bmesh.new()
        uv_layer = bm.loops.layers.uv.new("UVMap")
        top, bot = [], []
        for (x, s) in outline:
            y = 0.42 + s
            t = 0.13 * (1 - 0.78 * s / span)
            z = zf + s * dih
            top.append(bm.verts.new((x, side * y, z + t / 2)))
            bot.append(bm.verts.new((x, side * y, z - t / 2)))
        ft = bm.faces.new(top if side > 0 else list(reversed(top)))
        fb = bm.faces.new(list(reversed(bot)) if side > 0 else bot)
        nn = len(outline)
        for i in range(nn):
            a, b = i, (i + 1) % nn
            quad = (top[a], top[b], bot[b], bot[a]) if side < 0 else (top[b], top[a], bot[a], bot[b])
            bm.faces.new(quad)
        xs_, ys_ = [v.co.x for v in top], [v.co.y for v in top]
        x0, x1, y0, y1 = min(xs_), max(xs_), min(ys_), max(ys_)
        for f in bm.faces:
            for l in f.loops:
                l[uv_layer].uv = ((l.vert.co.x - x0) / (x1 - x0), (l.vert.co.y - y0) / (y1 - y0))
        bmesh.ops.recalc_face_normals(bm, faces=bm.faces[:])
        fin = new_obj(nm, bm, HULL, smooth=False)
        add_bevel(fin, 0.035, 4, 20)
        hull_parts.append(fin)
        # nav light at tip
        nl = prim_sphere(nm + "Light", 0.03, mats["Beacon"], 12, 6)
        nl.location = (xs - 0.36 * K, side * (0.42 + 1.44 * K), zf + 1.44 * K * dih)
        misc["emissive"].append(nl)
        # thruster pod under fin root
        pod = prim_cylinder(nm + "Pod", 0.075, 0.075, 0.55, 20, HULL)
        pod.location = (xs - 0.22, side * 0.74, STERN_R.c - 0.17)
        add_bevel(pod, 0.02, 3, 40)
        hull_parts.append(pod)
        pl = prim_cylinder(nm + "PodGlow", 0.05, 0.05, 0.02, 16, mats["Engine"])
        pl.location = (xs - 0.235, side * 0.74, STERN_R.c - 0.17)
        misc["emissive"].append(pl)

    # --- engine bay in the flat stern end
    a, b = STERN_R.w * CAP_F0 * 0.86, STERN_R.h * CAP_F0 * 0.86
    cz = STERN_R.c
    collar = prim_torus("BayCollar", 1.0, 0.03, 56, 10, HULL, scale_y=a, scale_z=b)
    collar.location = (X_STERN_END, 0, cz)
    hull_parts.append(collar)
    # bay wall + floor (loft from collar inward)
    bay_rings = []
    for x, sc in ((X_STERN_END + 0.01, 1.0), (X_STERN_END + 0.24, 0.9)):
        bay_rings.append([Vector((x, a * sc * math.sin(2 * math.pi * j / 56), cz + b * sc * math.cos(2 * math.pi * j / 56))) for j in range(56)])
    bay = loft("BayWall", bay_rings, closed=True, mat=HULL, cap_end=True)
    # flip: we look at the inside
    bay.data.flip_normals()
    hull_parts.append(bay)
    rn = STERN_R.h * CAP_F0 * 0.5
    for side, nm in ((1, "NozzleL"), (-1, "NozzleR")):
        nz = prim_cylinder(nm, rn * 0.72, rn, 0.34, 28, HULL, cap0=True, cap1=False)
        nz.location = (X_STERN_END + 0.24, side * a * 0.5, cz)
        nz.rotation_euler = (0, math.radians(180), 0)
        add_bevel(nz, 0.012, 2, 30)
        hull_parts.append(nz)
        inner = prim_cylinder(nm + "Inner", rn * 0.55, rn * 0.9, 0.26, 28, HULL, cap0=True, cap1=False)
        inner.location = (X_STERN_END + 0.2, side * a * 0.5, cz)
        inner.rotation_euler = (0, math.radians(180), 0)
        inner.data.flip_normals()
        hull_parts.append(inner)
        glow = prim_cylinder(nm + "Glow", rn * 0.66, rn * 0.66, 0.02, 24, mats["Engine"])
        glow.location = (X_STERN_END - 0.02, side * a * 0.5, cz)
        misc["emissive"].append(glow)

    # --- greebles on the stern deck: antenna array
    zdeck = wz(hullTopAt(150)) + 0.035
    for k, (x, y, h, r) in enumerate(((148, 0.18, 0.30, 0.012), (140, -0.14, 0.22, 0.010), (158, -0.05, 0.42, 0.014), (132, 0.05, 0.16, 0.009))):
        ant = prim_cylinder(f"Ant{k}", r, r * 0.7, h, 10, HULL)
        ant.rotation_euler = (0, math.radians(-90), 0)
        ant.location = (wx(x), y, zdeck - 0.02)
        hull_parts.append(ant)
        tip = prim_sphere(f"AntTip{k}", r * 1.6, mats["Beacon"] if k == 2 else HULL, 10, 6)
        tip.location = (wx(x), y, zdeck - 0.02 + h)
        (misc["emissive"] if k == 2 else hull_parts).append(tip)
    dish = prim_cylinder("Dish", 0.02, 0.11, 0.06, 20, HULL)
    dish.rotation_euler = (0, math.radians(-60), 0)
    dish.location = (wx(150), 0.30, zdeck + 0.06)
    hull_parts.append(dish)
    dishpost = prim_cylinder("DishPost", 0.012, 0.012, 0.09, 8, HULL)
    dishpost.rotation_euler = (0, math.radians(-90), 0)
    dishpost.location = (wx(150), 0.30, zdeck - 0.02)
    hull_parts.append(dishpost)
    for k, (x, y, sx, sy, sz) in enumerate(((165, 0.4, 0.22, 0.10, 0.06), (135, 0.52, 0.14, 0.14, 0.05), (128, -0.38, 0.18, 0.12, 0.07), (160, -0.5, 0.12, 0.08, 0.05))):
        bx = prim_box(f"Greeble{k}", sx, sy, sz, HULL)
        zb = wz(hullTopAt(x)) + 0.025
        bx.location = (wx(x), y, zb)
        add_bevel(bx, 0.008, 2)
        hull_parts.append(bx)
    # hull side sensor pods (mid-ship)
    for side in (1, -1):
        for k, x in enumerate((420, 700)):
            r = hull_ring(x)
            p = ring_pt(r, side * math.radians(70))
            pod = prim_box(f"SidePod{k}{'L' if side>0 else 'R'}", 0.36, 0.06, 0.09, HULL)
            pod.location = p + ring_normal2d(r, side * math.radians(70)) * 0.0
            add_bevel(pod, 0.012, 2)
            hull_parts.append(pod)

    # --- UV layout for detail parts (smart project into sub-rects)
    detail_parts = [o for o in hull_parts if o.name not in ("HullBody", "Deck", "Superstructure") and not o.name.startswith("Belt")]
    big = {"FinL": (0.0, 0.14, 0.5, 0.27), "FinR": (0.5, 0.14, 1.0, 0.27)}
    cells = [o for o in detail_parts if o.name not in big]
    cols = 8
    rows = math.ceil(len(cells) / cols)
    for idx, o in enumerate(cells):
        cx, cy = idx % cols, idx // cols
        cw, ch = 1.0 / cols, 0.135 / rows
        smart_uv(o, (cx * cw + 0.004, cy * ch + 0.004, (cx + 1) * cw - 0.004, (cy + 1) * ch - 0.004))
    for nm, rect in big.items():
        smart_uv(bpy.data.objects[nm], rect)

    # --- apply modifiers; build high-poly copies before joining
    hi_parts = []
    for o in hull_parts:
        select_only([o])
        bpy.ops.object.duplicate()
        h = bpy.context.active_object
        h.name = o.name + "_hi"
        for md in h.modifiers:
            if md.type == 'SUBSURF':
                md.levels = 3
            elif md.type == 'BEVEL':
                md.segments = max(md.segments, 4)
        if o.name in ("HullBody", "Deck", "Superstructure") or o.name.startswith("Belt"):
            pass
        else:
            if not any(md.type == 'SUBSURF' for md in h.modifiers) and o.name not in ("Dome",):
                add_subsurf(h, 1)
        apply_all(h)
        hi_parts.append(h)
        apply_all(o)

    # --- ring seams cut into the high-poly hull body (boolean)
    hull_hi = bpy.data.objects["HullBody_hi"]
    cutters = []
    for k, svgX in enumerate((205, 330, 470, 610, 745, 862)):
        r = hull_ring(svgX)
        w = 0.014
        rr = []
        for x, sc in ((r.x - w, 1.06), (r.x + w, 1.06), (r.x + w, 0.982), (r.x - w, 0.982)):
            rr.append([Vector((x, 0, r.c)) + (ring_pt(r, 2 * math.pi * j / 64) - Vector((r.x, 0, r.c))) * sc for j in range(64)])
        rr.append(rr[0])
        c = loft(f"Cutter{k}", rr, closed=True)
        cutters.append(c)
    select_only(cutters)
    bpy.ops.object.join()
    cutter = bpy.context.active_object
    cutter.name = "SeamCutter"
    md = hull_hi.modifiers.new("Seams", 'BOOLEAN')
    md.operation = 'DIFFERENCE'
    md.solver = 'EXACT'
    md.object = cutter
    apply_all(hull_hi)
    bpy.data.objects.remove(cutter)

    # --- join low parts into one Hull object
    select_only(hull_parts, hull_parts[0])
    bpy.ops.object.join()
    low = bpy.context.active_object
    low.name = "Hull"
    low.data.name = "Hull"
    select_only(hi_parts, hi_parts[0])
    bpy.ops.object.join()
    hi = bpy.context.active_object
    hi.name = "Hull_hi"
    hi.hide_render = True
    hi.hide_viewport = True

    # --- windows placed on the real (evaluated) surface via ray casts
    depsgraph = bpy.context.evaluated_depsgraph_get()
    bvh = BVHTree.FromObject(low, depsgraph)
    def surface(p, n):
        loc, nrm, idx, dist = bvh.ray_cast(p + n * 0.25, -n, 0.6)
        if loc is None:
            return None, None
        return loc, nrm

    win_specs = []  # (loc, normal, w, h)
    def in_bow(x, y):
        if x <= 880:
            return True
        xf = bowFrontAt(y)
        return xf is not None and x <= xf - 18
    pitch = 11
    rng = random.Random(0x41784f4d)
    for deg in (62, 79, 96, 113):
        th = math.radians(deg)
        x = 128
        while x <= 900:
            r = hull_ring(x)
            if r and r.h >= 0.16 * K:
                for side in (1, -1):
                    yy = ring_pt(r, side * th)
                    if in_bow(x, SVG_AXIS_Y - yy.z / S):
                        p, n = surface(yy, ring_normal2d(r, side * th))
                        if p:
                            if rng.random() > 0.05: win_specs.append((p, n, 0.05, 0.032))
            x += pitch
    rows = ((46, 0.42 * K, 0.046, 0.04), (64, 0.30 * K, 0.05, 0.034), (82, 0.16 * K, 0.05, 0.03))
    for deg, minH, ww, hh in rows:
        th = math.radians(deg)
        x = 185
        while x <= 880:
            r = super_ring(x)
            if r and r.h >= minH:
                for side in (1, -1):
                    p0 = ring_pt(r, side * th)
                    n0 = ring_normal2d(r, side * th)
                    p, n = surface(p0, n0)
                    if p and p.z > r.c + 0.02 and rng.random() > 0.05:
                        win_specs.append((p, n, ww, hh))
            x += pitch
    # pane mesh (low, emissive) + frame mesh (high only)
    bmw = bmesh.new(); uvw = bmw.loops.layers.uv.new("UVMap")
    bmf = bmesh.new(); uvf = bmf.loops.layers.uv.new("UVMap")
    for (p, n, ww, hh) in win_specs:
        tx = Vector((1, 0, 0)); tx = (tx - n * tx.dot(n)).normalized()
        tz = n.cross(tx).normalized()
        c = p + n * 0.004
        vs = [bmw.verts.new(c + tx * sx * ww / 2 + tz * sz * hh / 2) for sx, sz in ((-1, -1), (1, -1), (1, 1), (-1, 1))]
        f = bmw.faces.new(vs)
        for i, l in enumerate(f.loops):
            l[uvw].uv = ((i in (1, 2)) * 1.0, (i in (2, 3)) * 1.0)
        # frame: ring around the pane, raised 0.009
        fw, fh, rim = ww + 0.012, hh + 0.012, 0.007
        def rect(w_, h_, lift):
            cc = p + n * lift
            return [bmf.verts.new(cc + tx * sx * w_ / 2 + tz * sz * h_ / 2) for sx, sz in ((-1, -1), (1, -1), (1, 1), (-1, 1))]
        oi, oo, ii, io = rect(fw, fh, -0.004), rect(fw, fh, rim), rect(ww + 0.004, hh + 0.004, rim), rect(ww + 0.004, hh + 0.004, -0.004)
        for i in range(4):
            j = (i + 1) % 4
            bmf.faces.new((oi[i], oi[j], oo[j], oo[i]))
            bmf.faces.new((oo[i], oo[j], ii[j], ii[i]))
            bmf.faces.new((ii[i], ii[j], io[j], io[i]))
    bmesh.ops.recalc_face_normals(bmw, faces=bmw.faces[:])
    bmesh.ops.recalc_face_normals(bmf, faces=bmf.faces[:])
    windows = new_obj("Windows", bmw, WIN, smooth=False)
    frames = new_obj("WindowFrames_hi", bmf, HULL, smooth=False)
    frames.hide_render = True; frames.hide_viewport = True
    misc["window_count"] = len(win_specs)

    # --- panoramic bow band: one continuous strip wrapped around the nose (ray-cast onto the hull)
    x_ref = wx(892)
    zc_band = wz(266)
    half_band = 0.15
    rows_v = [-1, -0.5, 0, 0.5, 1]
    nphi = 44
    bmb = bmesh.new(); uvb = bmb.loops.layers.uv.new("UVMap")
    grid = []
    for i in range(nphi + 1):
        phi = -math.pi / 2 + math.pi * i / nphi
        d = Vector((math.cos(phi), math.sin(phi), 0))
        col = []
        for v in rows_v:
            z = zc_band + v * half_band
            origin = Vector((x_ref, 0, z)) + d * 5
            loc, nrm, _, _ = bvh.ray_cast(origin, -d, 10)
            if loc is None:
                loc, nrm = origin - d * 5, d
            col.append(loc + nrm * 0.007)
        grid.append(col)
    verts = [[bmb.verts.new(p) for p in col] for col in grid]
    for i in range(nphi):
        for j in range(len(rows_v) - 1):
            a, b, c, d_ = verts[i][j], verts[i][j + 1], verts[i + 1][j + 1], verts[i + 1][j]
            f = bmb.faces.new((a, d_, c, b))
            for k, l in enumerate(f.loops):
                l[uvb].uv = ((i + (k in (1, 2))) / nphi, (j + (k in (2, 3))) / 4)
    bmesh.ops.recalc_face_normals(bmb, faces=bmb.faces[:])
    band = new_obj("BowBand", bmb, mats["BowBand"])
    misc["emissive"].append(band)

    # --- join emissive helpers by material
    groups = {}
    for o in misc["emissive"]:
        groups.setdefault(o.data.materials[0].name, []).append(o)
    for mname, obs in groups.items():
        for o in obs:
            apply_all(o)
        select_only(obs, obs[0])
        bpy.ops.object.join()
        j = bpy.context.active_object
        j.name = mname
        j.data.name = mname
    windows.name = "Windows"

    # --- smooth shading by angle on the low-poly hull
    select_only([low])
    bpy.ops.object.shade_smooth_by_angle(angle=math.radians(38))
    select_only([bpy.data.objects["Engine"]]); bpy.ops.object.shade_smooth_by_angle(angle=math.radians(40))
    select_only([bpy.data.objects["Beacon"]]); bpy.ops.object.shade_smooth()

    # --- center on origin (hull length only), report
    cx = (wx(SVG_NOSE_X) + X_STERN_END) / 2
    for o in bpy.data.objects:
        if o.type == 'MESH':
            for v in o.data.vertices:
                v.co.x -= cx
    tri = sum(len(p.vertices) - 2 for o in bpy.data.objects if o.type == 'MESH' and not o.hide_render for p in o.data.polygons)
    print(f"[build] windows={misc['window_count']} lowpoly tris={tri} hi tris={sum(len(p.vertices)-2 for p in hi.data.polygons)}")
    setup_scene()
    bpy.ops.wm.save_as_mainfile(filepath=os.path.join(WORK, "axiom.blend"))

# ------------------------------------------------------------------ scene, lights, camera, render
def setup_scene():
    scene = bpy.context.scene
    world = bpy.data.worlds.new("Space") if not bpy.data.worlds else bpy.data.worlds[0]
    scene.world = world
    world.use_nodes = True
    nt = world.node_tree
    nt.nodes.clear()
    out = nt.nodes.new("ShaderNodeOutputWorld")
    bg = nt.nodes.new("ShaderNodeBackground")
    grad = nt.nodes.new("ShaderNodeTexGradient")
    mapn = nt.nodes.new("ShaderNodeMapping")
    tc = nt.nodes.new("ShaderNodeTexCoord")
    ramp = nt.nodes.new("ShaderNodeValToRGB")
    ramp.color_ramp.elements[0].color = (0.010, 0.012, 0.022, 1)
    ramp.color_ramp.elements[1].color = (0.10, 0.13, 0.22, 1)
    mapn.inputs["Rotation"].default_value = (0, math.radians(90), 0)
    nt.links.new(tc.outputs["Generated"], mapn.inputs["Vector"])
    nt.links.new(mapn.outputs["Vector"], grad.inputs["Vector"])
    nt.links.new(grad.outputs["Fac"], ramp.inputs["Fac"])
    nt.links.new(ramp.outputs["Color"], bg.inputs["Color"])
    bg.inputs["Strength"].default_value = 0.5
    nt.links.new(bg.outputs["Background"], out.inputs["Surface"])
    world.light_settings.distance = 1.2

    def sun(name, color, energy, from_dir):
        ld = bpy.data.lights.new(name, 'SUN')
        ld.color = color
        ld.energy = energy
        ld.angle = math.radians(3.0)
        lo = bpy.data.objects.new(name, ld)
        scene.collection.objects.link(lo)
        lo.rotation_euler = (-Vector(from_dir)).to_track_quat('-Z', 'Y').to_euler()
        return lo
    # warm key from the stern side (-X), camera side (-Y), above
    sun("Key", srgb("#ffdcae")[:3], 3.4, (-0.75, -0.45, 0.5))
    # cool rim from behind the bow (+X), far side, above
    sun("Rim", srgb("#a9c0ff")[:3], 5.0, (0.8, 0.55, 0.35))
    sun("Fill", srgb("#8090b0")[:3], 0.45, (0.1, -1.0, -0.4))
    # render-only engine glow light
    ld = bpy.data.lights.new("EngineLight", 'POINT'); ld.color = srgb(ACCENT)[:3]; ld.energy = 90; ld.shadow_soft_size = 0.5
    lo = bpy.data.objects.new("EngineLight", ld); scene.collection.objects.link(lo)
    lo.location = (X_STERN_END - (wx(SVG_NOSE_X) + X_STERN_END) / 2 - 0.5, 0, STERN_R.c)

    cam = bpy.data.cameras.new("Cam")
    cam.lens = 50
    cam.sensor_width = 36
    co = bpy.data.objects.new("Cam", cam)
    scene.collection.objects.link(co)
    scene.camera = co
    place_camera("main")

    scene.render.engine = 'BLENDER_EEVEE_NEXT'
    scene.render.resolution_x, scene.render.resolution_y = 1600, 900
    scene.render.film_transparent = False
    scene.eevee.taa_render_samples = 96
    scene.eevee.use_raytracing = True
    scene.eevee.use_shadows = True
    scene.eevee.shadow_ray_count = 2
    scene.eevee.shadow_step_count = 4
    scene.view_settings.view_transform = 'AgX'
    scene.view_settings.look = 'AgX - Medium High Contrast'
    scene.view_settings.exposure = 0.3
    scene.render.image_settings.file_format = 'PNG'
    # compositor bloom
    scene.use_nodes = True
    ct = scene.node_tree
    ct.nodes.clear()
    rl = ct.nodes.new("CompositorNodeRLayers")
    gl = ct.nodes.new("CompositorNodeGlare")
    gl.glare_type = 'BLOOM'
    gl.threshold = 1.2
    gl.mix = -0.35
    gl.size = 7
    comp = ct.nodes.new("CompositorNodeComposite")
    ct.links.new(rl.outputs["Image"], gl.inputs["Image"])
    ct.links.new(gl.outputs["Image"], comp.inputs["Image"])
    scene.render.use_compositing = True

def place_camera(view):
    co = bpy.context.scene.camera
    if view == "main":
        loc, aim, lens = Vector((6.2, -13.6, 3.9)), Vector((0.3, 0, 0.1)), 50
    elif view == "bow":
        loc, aim, lens = Vector((8.5, -5.0, 2.2)), Vector((3.6, 0, 0.1)), 60
    elif view == "stern":
        loc, aim, lens = Vector((-9.5, -5.5, 1.6)), Vector((-4.2, 0, -0.1)), 55
    elif view == "top":
        loc, aim, lens = Vector((2.0, -9.0, 9.0)), Vector((0.2, 0, 0.0)), 50
    co.location = loc
    co.rotation_euler = (aim - loc).to_track_quat('-Z', 'Y').to_euler()
    co.data.lens = lens

def render(tag, views=("main", "bow", "stern", "top")):
    scene = bpy.context.scene
    for v in views:
        place_camera(v)
        suffix = "" if v == "main" else f"-{v}"
        scene.render.filepath = os.path.join(WORK, f"render-{tag}{suffix}.png")
        bpy.ops.render.render(write_still=True)
        print("[render]", scene.render.filepath)

# ------------------------------------------------------------------ baking
def bake(res=4096, out_res=2048, ao_samples=48):
    scene = bpy.context.scene
    low = bpy.data.objects["Hull"]
    hi = bpy.data.objects["Hull_hi"]
    frames = bpy.data.objects["WindowFrames_hi"]
    windows = bpy.data.objects["Windows"]
    band = bpy.data.objects["BowBand"]
    hull_mat = bpy.data.materials["Hull"]

    # high-poly hull material: same look + procedural bump (longitudinal seams, plates, bow-band recess)
    hi_mat = bpy.data.materials.new("Hull_hi")
    bsdf = principled_tree(hi_mat)
    nt = hi_mat.node_tree
    bsdf.inputs["Base Color"].default_value = srgb("#d6dae1")
    bsdf.inputs["Roughness"].default_value = 0.55
    tc = nt.nodes.new("ShaderNodeTexCoord")
    sep = nt.nodes.new("ShaderNodeSeparateXYZ")
    nt.links.new(tc.outputs["Object"], sep.inputs["Vector"])
    # angle around the ship axis (Y,Z) -> pseudo-cylindrical coords
    ang = nt.nodes.new("ShaderNodeMath"); ang.operation = 'ARCTAN2'
    zc = nt.nodes.new("ShaderNodeMath"); zc.operation = 'ADD'; zc.inputs[1].default_value = 0.05
    nt.links.new(sep.outputs["Z"], zc.inputs[0])
    nt.links.new(sep.outputs["Y"], ang.inputs[0])
    nt.links.new(zc.outputs["Value"], ang.inputs[1])
    comb = nt.nodes.new("ShaderNodeCombineXYZ")
    angs = nt.nodes.new("ShaderNodeMath"); angs.operation = 'MULTIPLY'; angs.inputs[1].default_value = 0.9
    nt.links.new(ang.outputs["Value"], angs.inputs[0])
    nt.links.new(sep.outputs["X"], comb.inputs["X"])
    nt.links.new(angs.outputs["Value"], comb.inputs["Y"])
    brick = nt.nodes.new("ShaderNodeTexBrick")
    brick.inputs["Scale"].default_value = 1.0
    brick.inputs["Mortar Size"].default_value = 0.012
    brick.inputs["Mortar Smooth"].default_value = 0.6
    brick.inputs["Bias"].default_value = 0.0
    brick.inputs["Brick Width"].default_value = 0.95
    brick.inputs["Row Height"].default_value = 0.42
    brick.offset = 0.5
    brick.inputs["Color1"].default_value = (0.5, 0.5, 0.5, 1)
    brick.inputs["Color2"].default_value = (0.56, 0.56, 0.56, 1)
    brick.inputs["Mortar"].default_value = (0.35, 0.35, 0.35, 1)
    nt.links.new(comb.outputs["Vector"], brick.inputs["Vector"])
    # longitudinal seam lines at fixed angles: cos(k*angle) peaks
    lon = nt.nodes.new("ShaderNodeMath"); lon.operation = 'MULTIPLY'; lon.inputs[1].default_value = 3.0
    nt.links.new(ang.outputs["Value"], lon.inputs[0])
    lonc = nt.nodes.new("ShaderNodeMath"); lonc.operation = 'COSINE'
    nt.links.new(lon.outputs["Value"], lonc.inputs[0])
    lonp = nt.nodes.new("ShaderNodeMath"); lonp.operation = 'GREATER_THAN'; lonp.inputs[1].default_value = 0.9975
    nt.links.new(lonc.outputs["Value"], lonp.inputs[0])
    lonw = nt.nodes.new("ShaderNodeMath"); lonw.operation = 'MULTIPLY'; lonw.inputs[1].default_value = -0.35
    nt.links.new(lonp.outputs["Value"], lonw.inputs[0])
    # bow band recess: x > band start and |z - c| < 0.17
    bx = nt.nodes.new("ShaderNodeMath"); bx.operation = 'GREATER_THAN'; bx.inputs[1].default_value = wx(890) - (wx(SVG_NOSE_X) + X_STERN_END) / 2
    nt.links.new(sep.outputs["X"], bx.inputs[0])
    bz = nt.nodes.new("ShaderNodeMath"); bz.operation = 'ABSOLUTE'
    bzc = nt.nodes.new("ShaderNodeMath"); bzc.operation = 'SUBTRACT'; bzc.inputs[1].default_value = wz(264)
    nt.links.new(sep.outputs["Z"], bzc.inputs[0]); nt.links.new(bzc.outputs["Value"], bz.inputs[0])
    bzl = nt.nodes.new("ShaderNodeMath"); bzl.operation = 'LESS_THAN'; bzl.inputs[1].default_value = 0.175
    nt.links.new(bz.outputs["Value"], bzl.inputs[0])
    bband = nt.nodes.new("ShaderNodeMath"); bband.operation = 'MULTIPLY'
    nt.links.new(bx.outputs["Value"], bband.inputs[0]); nt.links.new(bzl.outputs["Value"], bband.inputs[1])
    bbw = nt.nodes.new("ShaderNodeMath"); bbw.operation = 'MULTIPLY'; bbw.inputs[1].default_value = -0.6
    nt.links.new(bband.outputs["Value"], bbw.inputs[0])
    h1 = nt.nodes.new("ShaderNodeMath"); h1.operation = 'ADD'
    nt.links.new(brick.outputs["Color"], h1.inputs[0]); nt.links.new(lonw.outputs["Value"], h1.inputs[1])
    h2 = nt.nodes.new("ShaderNodeMath"); h2.operation = 'ADD'
    nt.links.new(h1.outputs["Value"], h2.inputs[0]); nt.links.new(bbw.outputs["Value"], h2.inputs[1])
    bump = nt.nodes.new("ShaderNodeBump")
    bump.inputs["Strength"].default_value = 0.45
    bump.inputs["Distance"].default_value = 0.02
    nt.links.new(h2.outputs["Value"], bump.inputs["Height"])
    nt.links.new(bump.outputs["Normal"], bsdf.inputs["Normal"])
    hi.data.materials.clear(); hi.data.materials.append(hi_mat)
    frames.data.materials.clear(); frames.data.materials.append(hi_mat)

    # bake target images
    def new_img(name, data=False):
        img = bpy.data.images.new(name, res, res, alpha=False, float_buffer=False)
        if data:
            img.colorspace_settings.name = 'Non-Color'
        img.generated_color = (0, 0, 0, 1)
        return img
    img_ao, img_n, img_e = new_img("bake_ao", True), new_img("bake_normal", True), new_img("bake_emissive", False)
    nt = hull_mat.node_tree
    tex = nt.nodes.new("ShaderNodeTexImage")
    tex.name = "BakeTarget"
    nt.nodes.active = tex

    scene.render.engine = 'CYCLES'
    scene.cycles.device = 'CPU'
    scene.cycles.use_denoising = False
    bk = scene.render.bake
    bk.use_selected_to_active = True
    bk.use_cage = False
    bk.cage_extrusion = 0.035
    bk.max_ray_distance = 0.25
    bk.margin = 12
    bk.margin_type = 'EXTEND'
    bk.use_pass_direct = False; bk.use_pass_indirect = False
    for o in (hi, frames):
        o.hide_render = False; o.hide_viewport = False
    low.visible_diffuse = False; low.visible_glossy = False; low.visible_shadow = False; low.visible_camera = False

    def do_bake(img, btype, samples, extra):
        tex.image = img
        scene.cycles.samples = samples
        select_only(extra + [low], low)
        for o in extra: o.hide_render = False
        bpy.ops.object.bake(type=btype, use_selected_to_active=True, cage_extrusion=0.035, max_ray_distance=0.25, margin=12, **({"normal_space": 'TANGENT'} if btype == 'NORMAL' else {}))
        img.filepath_raw = os.path.join(WORK, f"{img.name}_{res}.png")
        img.file_format = 'PNG'
        img.save()
        print("[bake]", btype, img.filepath_raw)

    windows.hide_render = True
    band.hide_render = True
    do_bake(img_n, 'NORMAL', 8, [hi, frames])
    do_bake(img_ao, 'AO', ao_samples, [hi, frames])
    windows.hide_render = False
    band.hide_render = False
    do_bake(img_e, 'EMIT', 4, [hi, frames, windows, band])
    windows.hide_render = False
    low.visible_diffuse = True; low.visible_glossy = True; low.visible_shadow = True; low.visible_camera = True
    for o in (hi, frames):
        o.hide_render = True; o.hide_viewport = True

    # compose ORM (R=AO, G=roughness, B=metallic) and downscale everything to out_res
    import numpy as np
    for img in (img_ao, img_n, img_e):
        img.scale(out_res, out_res)
    ao = np.empty(out_res * out_res * 4, dtype=np.float32)
    img_ao.pixels.foreach_get(ao)
    ao = ao.reshape(-1, 4)
    rng = np.random.default_rng(7)
    noise = rng.normal(0, 0.012, ao.shape[0]).astype(np.float32)
    orm = np.empty_like(ao)
    orm[:, 0] = np.clip(ao[:, 0], 0, 1)
    orm[:, 1] = np.clip(0.55 + 0.14 * (1 - ao[:, 0]) + noise, 0, 1)
    orm[:, 2] = 0.10
    orm[:, 3] = 1.0
    img_orm = bpy.data.images.new("axiom_orm", out_res, out_res, alpha=False)
    img_orm.colorspace_settings.name = 'Non-Color'
    img_orm.pixels.foreach_set(orm.ravel())
    for img, nm in ((img_orm, "axiom_orm"), (img_n, "axiom_normal"), (img_e, "axiom_emissive")):
        img.filepath_raw = os.path.join(WORK, nm + ".png")
        img.file_format = 'PNG'
        img.save()
        img.name = nm
    # rewire the Hull material for render + export
    nt.nodes.remove(tex)
    bsdf = nt.nodes["Principled BSDF"]
    t_orm = nt.nodes.new("ShaderNodeTexImage"); t_orm.image = img_orm; t_orm.image.colorspace_settings.name = 'Non-Color'
    t_n = nt.nodes.new("ShaderNodeTexImage"); t_n.image = img_n; t_n.image.colorspace_settings.name = 'Non-Color'
    t_e = nt.nodes.new("ShaderNodeTexImage"); t_e.image = img_e
    sepc = nt.nodes.new("ShaderNodeSeparateColor")
    nt.links.new(t_orm.outputs["Color"], sepc.inputs["Color"])
    nt.links.new(sepc.outputs["Green"], bsdf.inputs["Roughness"])
    nt.links.new(sepc.outputs["Blue"], bsdf.inputs["Metallic"])
    nm = nt.nodes.new("ShaderNodeNormalMap"); nm.inputs["Strength"].default_value = 1.0
    nt.links.new(t_n.outputs["Color"], nm.inputs["Color"])
    nt.links.new(nm.outputs["Normal"], bsdf.inputs["Normal"])
    nt.links.new(t_e.outputs["Color"], bsdf.inputs["Emission Color"])
    bsdf.inputs["Emission Strength"].default_value = 1.0
    # glTF occlusion via the exporter's settings group
    grp = bpy.data.node_groups.new("glTF Material Output", 'ShaderNodeTree')
    grp.interface.new_socket("Occlusion", in_out='INPUT', socket_type='NodeSocketFloat')
    gin = grp.nodes.new("NodeGroupInput")
    gnode = nt.nodes.new("ShaderNodeGroup"); gnode.node_tree = grp
    nt.links.new(sepc.outputs["Red"], gnode.inputs["Occlusion"])
    # AO also darkens the base colour a touch for the EEVEE preview (mix node) - keep base as factor for export
    scene.render.engine = 'BLENDER_EEVEE_NEXT'
    bpy.ops.wm.save_as_mainfile(filepath=os.path.join(WORK, "axiom-baked.blend"))

# ------------------------------------------------------------------ export
def export(path):
    for o in list(bpy.data.objects):
        if o.name.endswith("_hi") or o.type in ('LIGHT', 'CAMERA'):
            o.hide_render = True
    # neutral emissive strengths for the web (the app drives intensity)
    for nm in ("Windows", "BowBand", "Engine", "Beacon"):
        bpy.data.materials[nm].node_tree.nodes["Principled BSDF"].inputs["Emission Strength"].default_value = 1.0
    objs = [bpy.data.objects[n] for n in ("Hull", "Windows", "BowBand", "Engine", "Beacon")]
    select_only(objs, objs[0])
    os.makedirs(os.path.dirname(path), exist_ok=True)
    bpy.ops.export_scene.gltf(filepath=path, export_format='GLB', use_selection=True, export_apply=True,
                              export_texcoords=True, export_normals=True, export_materials='EXPORT',
                              export_image_format='WEBP', export_image_quality=88, export_yup=True,
                              export_tangents=False, export_animations=False, export_lights=False, export_cameras=False)
    print("[export]", path, os.path.getsize(path))

# ------------------------------------------------------------------ main
if __name__ == "__main__":
    i = 0
    while i < len(ARGS):
        a = ARGS[i]
        if a == "build":
            build()
        elif a == "open":
            bpy.ops.wm.open_mainfile(filepath=os.path.join(WORK, ARGS[i + 1])); i += 1
        elif a == "render":
            render(ARGS[i + 1]); i += 1
        elif a == "render1":
            render(ARGS[i + 1], ("main",)); i += 1
        elif a == "bake":
            bake()
        elif a == "bake_fast":
            bake(res=2048, out_res=2048, ao_samples=16)
        elif a == "export":
            export(ARGS[i + 1]); i += 1
        elif a == "save":
            bpy.ops.wm.save_as_mainfile(filepath=os.path.join(WORK, ARGS[i + 1])); i += 1
        i += 1
