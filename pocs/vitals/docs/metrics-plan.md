# vitals metrics plan — strain, recovery, sleep, stress, fitness age

What vitals will compute from the data your own strap records, how, and what the
evidence is. Everything here is **vitals' own** calculation from published
research. It is not WHOOP's algorithm and its numbers will not match WHOOP's
(see the ground rules in `whoop-reverse-engineering.md`: facts about the
protocol only, no proprietary scores).

Each method is tagged:

- **verified** — the formula or claim was read in the cited abstract or paper page
- **secondary** — seen only on a third-party page; check the primary source before coding it
- **own design** — a choice made here, to be tuned on real data

## Data policy: day zero

Today is the first day of data. vitals will not try to back-fill the strap's old history.

- A strap that lost its clock counts from 1971-01-01 at power-on and stamps stored records
  on that clock, so history from before the clock was set has no trustworthy dates.
  Data counts from the first time the clock is set and a sync completes (`SetClock`, then the history sync).
- Raw samples are kept (append-only, one file per local day under `$VITALS_HOME/strap/`), so every
  metric can be recomputed later when a method improves. Scores are derived, never the only copy.
- No score is shown before it has a baseline. Provisional from 7 nights, labelled "calibrating";
  full from 28 nights (the cyclist study set thresholds from four baseline weeks — see Recovery).
- Data stays on this machine.

## Foundations (needed by everything)

| Item | Method | Status |
|---|---|---|
| Max heart rate | `208 − 0.7 × age` (Tanaka, Monahan & Seals, J Am Coll Cardiol 2001;37:153-6). Not different for men and women, not changed by habitual activity. Individual scatter is large (SEE about 11-12 bpm in one study), so prefer the highest heart rate you actually record when it exceeds the estimate | verified |
| Resting HR | lowest sustained heart rate while asleep and still | own design |
| Clean RR | existing `internal/hrv.Clean` (300-2000 ms, artefact removal) | exists |

## Strain (cardiovascular load)

- **Edwards TRIMP:** minutes in five zones of %HRmax (50-60, 60-70, 70-80, 80-90, 90-100) times weights 1 to 5,
  summed. **verified** (zones and weights from a clinical-protocol description of Edwards 1993).
- **Banister TRIMP:** `sum( minutes × x × 0.64 × e^(k·x) )` with `x` = fraction of heart-rate reserve,
  `k` = 1.92 for men and 1.67 for women. **secondary**: the shape and the two exponents were found on
  several pages; the women's multiplier is commonly given as 0.86 but was not seen. Verify against
  Morton et al. 1990 / Banister 1991 before coding. The two methods correlated 0.80-0.99 in one study
  of 10 athletes (small sample).
- **Day strain:** TRIMP summed over the day, mapped to a 0-21 display scale by **own design**:
  logarithmic, scaled against your own recorded range (percentile of your last 28 days) so the scale
  means something for you. Works from live heart rate today; no history needed.
- Limits: heart rate alone misses non-cardio load (lifting, heat, stress).

## Recovery

- **Signal:** ln(RMSSD) from RR intervals recorded while asleep and still, plus resting HR and respiratory rate.
  RMSSD is already computed by `internal/hrv`.
- **Window:** the first ~4 hours after sleep onset or the whole-night mean. Polar documents roughly
  the first four hours of sleep for its recovery measure (**secondary**: manual and a blog). No source
  found supports a "last slow-wave" window, so don't assume one.
- **Baseline and trend (Plews/Buchheit approach):** compare the **7-day rolling mean of ln RMSSD** with your own
  baseline mean and **smallest worthwhile change** band. A 77-day triathlete case study and a Jyväskylä
  running trial used this; the trial used the band to choose hard or easy training days. **verified** for the
  rolling-mean-versus-SWC idea. The exact SWC convention (a fraction of the baseline SD, often quoted as 0.5 SD)
  was **not** confirmed; check Plews et al. before choosing a number. Group-level evidence that HRV-guided
  training beats planned training is mixed (one secondary summary of a 2021 meta-analysis), so describe this
  as a readiness trend, not a prescription.
- **Score:** 0-100 as the position of last night's ln RMSSD (with resting HR as a modifier) in your own baseline
  distribution. **own design.** Needs history (overnight RR).
- **Validity of the sensor:** wrist PPG nights agreed strongly with a chest reference in published checks
  (Polar Vantage V2 nocturnal ln RMSSD ICC 0.93 with a small positive bias; an Oura study r = 0.98 for RMSSD, in people
  with RMSSD 20-100 ms). Different devices, so treat as encouraging only; test against your own strap.

## Sleep

- **Sleep/wake from motion:** Cole-Kripke and Sadeh actigraphy algorithms. Against polysomnography on a
  wrist device, Cole-Kripke was more sensitive to sleep (0.88-0.96) but less specific to wake (0.35-0.64) than Sadeh
  (0.82-0.91 and 0.47-0.68). Total sleep time agreed reasonably; wake after sleep onset agreed poorly (**verified**
  from a validation study). The original epoch weights were **not** retrieved; read the 1992 and 1994 papers (Sleep) first.
- **Stages from heart rate/HRV + motion:** published neural-network work on accelerometer plus inter-beat
  intervals reports Cohen's kappa about 0.64 and 78% accuracy for four classes in 30 s epochs (**verified**,
  abstract-level). Expect rough stage estimates, good totals.
- **Free offline test data:** the PhysioNet `sleep-accel` set (Walch et al., SLEEP 2019): 31 subjects with wrist motion,
  heart rate and polysomnography labels. Use it to test our sleep code before trusting it on your strap. **verified**.
- Needs history including motion. The accelerometer fields in the history record are only a guess so far
  (see `whoop-protocol-findings.md`).

## Stress

- **Baevsky stress index** from the RR histogram (mode, mode amplitude, range). It is sensitive to bin width,
  so a fixed bin width is required for comparability, and published normal ranges are inconsistent (**verified**
  from a 2024 paper's own caveat). So vitals reports stress **relative to your own baseline**, never an absolute band.
- **Own design:** stress is flagged when, while you are still, RMSSD is well below your baseline and heart rate above it.
  Needs a continuous RR series, so it depends on history sync (realtime RR covered only 13% of beats).

## Fitness age (not "biological age")

- **VO2max estimate:** `15.3 × HRmax / HRrest` (Uth et al., 2004). Error: SEE 2.7 ml/kg/min with a measured HRmax,
  4.7 with an age-predicted one. Derived from 46 well-trained men aged 21-51; a later revision suggests a constant
  of about 14 for typical 40-year-old men (Voutilainen 2020, **secondary**). Women's value of 14.5 seen only on a commercial calculator. **verified** for the original.
- **Fitness age:** the age at which the population median VO2max equals yours, using HUNT-study norms. The Nes 2011
  equation and the norms table were **not** retrieved; get them from the primary source (NTNU / Nes, Loe, Wisløff)
  before building this. Until then do not show an age.
- Requires your age and sex, stored locally. Heart rate alone cannot give a validated biological age; label it
  "estimated cardiorespiratory fitness age".

## Others

| Metric | Method | Status |
|---|---|---|
| Calories | Keytel et al., J Sports Sci 2005;23:289-97, no-VO2max model (r = 0.857). Coefficients seen only on third-party pages and they disagree on units | **secondary** — do not code until read in the paper |
| Respiratory rate | peak of the 0.1-0.5 Hz respiratory component in the RR series (respiratory sinus arrhythmia); correlations 0.68-0.89 with reference at rest in one validation, lower than ECG-based | verified; needs a complete RR series |
| Skin temperature, blood oxygen | raw values are in the history records but not decoded; unit conversions are protocol facts and can be taken from the app | open |
| Steps | motion peaks from the accelerometer | open, depends on decoding the record |

## Build order

1. `SetClock` test on the strap (button is in the dashboard).
2. History sync: pull, persist, ack with the 8-byte token, in that order (the ack lets the strap free that memory).
3. Day-zero raw store, then strain (works from live heart rate).
4. Recovery, resting HR and respiratory rate (needs overnight history and a week of baseline).
5. Sleep, then stress.
6. Fitness age once the norms source is read.

## Still to read from primary sources

Banister/Morton constants; the SWC convention in Plews et al.; the Cole-Kripke and Sadeh weights; the Keytel
coefficients; the HUNT fitness-age equation and norms.
