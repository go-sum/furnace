---
title: UI Design Guide
description: "UI design purpose, component library scope, design principles, typography scale, font rules, color palette, semantic color, layout grid, spacing scale, elevation shadows, images, finishing touches, core components, button variants, data display, feedback components, layout components, form components, interactive components, view composition, layout templates, page views, partials, error pages, practical UI rules, accessibility, decision checklist"
weight: 30
---

# UI Design Guide

> This guide defines how UI should be designed and composed in this application.
> It is the visual companion to [`CLAUDE.md`](../CLAUDE.md) and the
> implementation companion to [`ARCHITECTURE_GUIDE.md`](./ARCHITECTURE_GUIDE.md).
>
> It incorporates the relevant design principles from [`tailwindcss.com`](https://tailwindcss.com/) 
> and one of the definitive guides on design [`Refactoring UI`](https://refactoringui.com/) 
> by Adam Wathan & Steve Schoger, adapted to the application's current UI surface and
> the `github.com/go-sum/foundry/pkg/componentry` package layout. This guide should stand on its own
> without requiring the source text.
>
> The guidance is tailored to:
>
> - reusable components in `github.com/go-sum/foundry/pkg/componentry/ui/`
> - form controls in `github.com/go-sum/foundry/pkg/componentry/form/`
> - higher-level interaction and HTMX helpers in
>   `github.com/go-sum/foundry/pkg/componentry/interactive/` and
>   `github.com/go-sum/foundry/pkg/componentry/patterns/`
> - app-specific composition in `starter/internal/view/` layered on top of
>   `pkg/web/viewstate/`

---

## 0. Quick Reference

- §1 Purpose: why this guide exists and how it fits the application
- §2 Scope: which packages and components are covered
- §3 Design Principles: 19 rules — start with features, hierarchy first, spacing, color, accessibility
- §4 Visual Language: typography scale, color palette, layout grid, spacing, elevation, images
- §5 Component Library: core, data, feedback, layout, form, interactive, patterns, examples
- §6 View Composition: layout templates, page views, partials, error pages
- §7 Practical Rules: when to use shared components, composition over variants, form readability
- §8 Decision Checklist: questions to answer before shipping new UI
- §9 Reference Map: quick lookup for patterns and components

## 1. Purpose and Goals

This application targets high-performance modern web applications rendered
primarily on the server. The UI should therefore feel:

- clear before it feels decorative
- fast before it feels clever
- reusable before it becomes page-specific
- consistent across full-page views and HTMX partials

The component library exists to make those goals cheap to achieve. Prefer the
shared components, patterns, and tokens over rebuilding visual patterns ad hoc
inside views.

---

## 2. Scope and Package Coverage

This guide covers:

- visual principles for components in `github.com/go-sum/foundry/pkg/componentry/ui/`
- how `github.com/go-sum/foundry/pkg/componentry/form`,
  `github.com/go-sum/foundry/pkg/componentry/interactive`, and
  `github.com/go-sum/foundry/pkg/componentry/patterns` should shape interface behavior
- how full pages and HTMX partials in `starter/internal/view/` should compose
  those pieces through `pkg/web/viewstate`
- the default spacing, typography, color, and elevation language of the app

This guide does not try to document every exported API. For exact props and
rendering behavior, read the package source and tests.

Primary code references:

- `pkg/showcase/componentry/`
- `github.com/go-sum/foundry/pkg/componentry/ui/core`
- `github.com/go-sum/foundry/pkg/componentry/ui/data`
- `github.com/go-sum/foundry/pkg/componentry/ui/feedback`
- `github.com/go-sum/foundry/pkg/componentry/ui/layout`
- `github.com/go-sum/foundry/pkg/componentry/form`
- `github.com/go-sum/foundry/pkg/componentry/interactive`
- `github.com/go-sum/foundry/pkg/componentry/patterns`
- `pkg/web/viewstate/layout/`
- `pkg/web/viewstate/errorpage/`
- `starter/internal/view/page/`
- `starter/internal/view/partial/`

---

## 3. Design Principles

### 3a. Start with a Feature, Not the Shell

The most important rule still applies: design around the job
the user is doing, not around abstract page chrome.

In this repo that means:

- design the user table before redesigning the navbar
- design the contact flow before inventing a new page template
- design the inline edit row before adding decorative wrappers

Good current examples:

- `starter/internal/view/page/home.go`
- `starter/internal/view/page/contact.go`
- `starter/internal/view/partial/contactpartial/contact_form.go`

The shell should emerge from repeated feature needs. It should not be the first
thing designed.

### 3b. Add Detail Later, Not Upfront

Do not begin by tuning shadows, icon sizes, borders, or decorative accents.

Start with:

- the user job
- the information they need
- the action they need to take
- the states the screen must support

Then refine:

- hierarchy
- spacing
- typography
- color
- depth

Design in grayscale first when exploring a new surface. Forcing spacing,
contrast, and size to carry the hierarchy produces a clearer result than
reaching for color too early.

### 3c. Ship the Smallest Useful Version First

Do not design or imply functionality that is not ready to build.

For new UI, build:

- the happy path first
- the minimum credible empty state
- the minimum credible error state
- the minimum credible loading state if the surface needs one

Then iterate on the real implementation. The repo already follows this pattern:
the users region supports listing, inline editing, loading, and empty state
without trying to solve every future admin workflow on day one.

### 3d. Work in Short Design Cycles

Do not try to design every edge case in the abstract before implementation.

Preferred loop:

1. sketch the simplest useful version
2. implement it in the real UI
3. exercise the working interface
4. refine only where real usage exposes friction

This application favors server-rendered HTML precisely because it makes iteration
cheap. Use that advantage.

### 3e. Choose a Visual Personality Deliberately

Every interface communicates a personality whether intended or not. The
default personality should be:

- clear
- competent
- modern
- understated

That personality is expressed through:

- restrained color usage
- consistent corner radius
- a small, purposeful type scale
- straightforward copy
- quiet but polished interaction states

If the product needs a different tone, change it centrally
through tokens, typography, and component defaults. Do not drift page by page.

### 3f. Limit User Choices on Purpose

Design quality improves when the system narrows the decision surface.

This application relies on predefined systems for:

- typography
- spacing
- semantic color
- elevation
- widths and layout constraints
- component variants

When making a UI decision, choose from the system first. If the system feels
too small, extend it deliberately; do not bypass it with arbitrary one-off
values.

### 3g. Visual Hierarchy Before Decoration

Most emphasis should come from:

- spacing
- font weight
- restrained text-size changes
- muted versus foreground text
- placement

Do not reach for extra borders, badges, or colors first.

Current hierarchy defaults:

- page title: `text-2xl font-bold`
- card title: `text-lg font-semibold`
- controls and body text: `text-sm`
- supporting text: `text-muted-foreground`
- badges and fine metadata: `text-xs`

Examples:

- `github.com/go-sum/foundry/pkg/componentry/ui/data` (card)
- `github.com/go-sum/foundry/pkg/componentry/ui/core` (button)
- `starter/internal/view/page/home.go`

### 3h. Size Alone Does Not Create Hierarchy

Users notice contrast, placement, and density before they notice a one-step
font-size change.

Prefer to emphasize by:

- increasing contrast
- isolating the important element with space
- simplifying nearby competing elements
- using weight or case deliberately

Before making something larger, ask if the surrounding content should instead
be quieter.

### 3i. Emphasize by De-emphasizing Surrounding Elements

When a screen feels noisy, the fix is usually not to make the important thing
louder. The fix is to soften everything that is less important.

Common patterns:

- row actions use `ghost` or destructive ghost buttons so table data dominates
- descriptions use `text-muted-foreground` so headings and values lead
- nav metadata and helper text stay quieter than primary actions

### 3j. Labels Are a Last Resort

In display UI, labels are secondary. The value is what the user came to see.

This means:

- labels in cards and detail views should usually be smaller and muted
- if context makes a value obvious, omit the label entirely
- repeated label-value noise should be simplified into layout or grouping

Forms still need explicit labels unless there is a deliberate accessible
alternative. Display UI usually does not.

### 3k. Separate Visual Hierarchy from Document Hierarchy

Choose semantic elements for structure and accessibility, then style them for
their visual role.

Examples:

- a page title can be an `h1` without looking like a marketing hero
- a card title can be an `h3` without feeling oversized
- a section heading can visually behave like a label

Do not let heading levels force a visual type scale that harms the page.

### 3l. Design in Grayscale First, Then Apply Semantic Color

Use semantic tokens only when they carry meaning:

- `primary` for the main action
- `destructive` for danger or error emphasis
- `secondary` or `muted` for lower emphasis
- `accent` for hover or focus surfaces

Do not introduce arbitrary palette classes in views when a semantic token or
component variant already exists.

### 3m. Keep the Spacing and Type Scale Tight

The app already leans on a small number of recurring sizes. Keep using them.

Recommended spacing rhythm:

- `gap-2` / `p-2` for dense controls and table cells
- `gap-3` for compact form flows
- `gap-4` for related blocks
- `p-4` for compact panels and alerts
- `p-6` for cards and major grouped content
- `py-6`, `py-12`, `py-16`, `py-24` for page-level breathing room

Recommended text rhythm:

- `text-xs` for badges and tiny metadata
- `text-sm` for most UI copy and controls
- `text-lg` for card titles
- `text-2xl` for page headings

If a design needs a large new ramp of spacing or typography values, simplify
the design before extending the system.

### 3n. Start with Generous White Space

When a UI feels cramped, the problem is usually layout density, not a missing
shadow or color accent.

Default bias:

- give forms, cards, and grouped sections more room first
- tighten only when density has a clear product reason

### 3o. More Space Around Groups Than Inside Them

This is one of the most important spacing rules in the system.

Examples:

- label-to-input spacing should be smaller than field-to-field spacing
- card title-to-description spacing should be tighter than card-to-card spacing
- row action gaps should be smaller than the distance to the next row

If intra-group and inter-group spacing are too similar, the UI becomes hard to
scan.

### 3p. Do Not Fill the Whole Screen by Default

You do not need to use the full available width just because it exists.

Preferred patterns:

- constrain forms with `max-w-sm` or `max-w-md`
- constrain descriptive content with `max-w-2xl` or similar
- use wider layouts only for tables, dashboards, and data-heavy surfaces

Most app tasks become easier when the content width is intentionally limited.

### 3q. Avoid Ambiguous Spacing

Spacing should communicate structure. If two gaps look the same, users will
assume the relationships are the same.

Be explicit about:

- whether a caption belongs to the field above or the section below
- whether row actions belong to a row or to the table as a whole
- whether helper text belongs to the card header or card body

Use spacing to answer those questions without needing extra borders.

### 3r. Use Depth (Shadows, Elevation) Sparingly

Depth is already encoded in the shared components:

- cards and many buttons use `shadow-xs`
- toasts use `shadow-md`
- drawers and overlays use stronger elevation

Use borders for separation and shadows for elevation. Do not stack both
aggressively everywhere.

### 3s. Accessibility Is Part of the Design Language

Accessible UI is the default, not a later pass.

Current patterns to preserve:

- focus-visible rings on buttons and inputs
- destructive color on invalid labels and fields
- `aria-describedby` and `aria-errormessage` wiring via form helpers
- correct announcement roles for alerts and toast surfaces
- semantic HTML tables, headings, forms, and navigation

Examples:

- `github.com/go-sum/foundry/pkg/componentry/ui/core` (button)
- `github.com/go-sum/foundry/pkg/componentry/form` (field)
- `github.com/go-sum/foundry/pkg/componentry/ui/feedback` (alert)
- `github.com/go-sum/foundry/pkg/componentry/interactive/pagination`

---

## 4. Visual Language System

### 4a. Typography Scale and Font Rules

Use a narrow, purposeful type scale:

- page headings: `text-2xl font-bold`
- section or card headings: `text-lg font-semibold`
- controls, paragraphs, table content: `text-sm`
- metadata and badges: `text-xs`

Supporting rules:

- prefer weight and contrast over large type jumps
- use tighter tracking for headings
- use muted text for descriptions, hints, empty states, and secondary metadata
- keep line lengths readable for prose
- keep large text line-height tighter than body text

#### Keep line length readable

For longer descriptive copy, constrain width rather than letting text fill the
layout indefinitely.

Use:

- narrow containers for forms and focused tasks
- constrained prose widths for explanations and help text
- wider containers only for data-heavy surfaces

#### Baseline, not center

When aligning text with icons, inputs, or adjacent blocks, bias toward optical
baseline alignment instead of geometric centering. Perfect centering often
looks wrong because text has different visual weight than boxes and icons.

#### Line height is proportional

Larger text needs less line height than small text:

- headings: `leading-tight` or `leading-snug`
- body text: `leading-normal` or `leading-relaxed`
- dense metadata: `leading-none` or `leading-tight`

A large heading with loose leading looks accidental. Body text with tight
leading becomes hard to read.

#### Use letter spacing intentionally

Letter spacing affects both readability and personality:

- headings benefit from slightly tighter tracking
- all-caps labels and overlines need looser tracking
- body text and controls should usually remain at default tracking

Do not add extra letter spacing to mixed-case body copy.

#### Not every link needs a color

Links inside navigation, menus, buttons, and structured UI can often inherit
surrounding text color and rely on placement, hover state, underline, or
weight for distinction.

Reserve obvious link color shifts for places where discoverability actually
needs help.

#### Align with readability in mind

Default alignment rules:

- left-align paragraphs, labels, and mixed text content
- center-align only short, intentionally centered compositions like hero or
  empty-state blocks
- right-align numeric table columns for comparison

### 4b. Color Palette and Semantic Color Usage

Use semantic color tokens already present in the design system:

- `bg-background`, `text-foreground`
- `bg-card`, `text-card-foreground`
- `text-muted-foreground`
- `bg-primary`, `text-primary-foreground`
- `bg-secondary`, `text-secondary-foreground`
- `bg-destructive`, `text-destructive`
- `hover:bg-accent`, `hover:text-accent-foreground`
- `border-border`, `border-input`, `border-ring`

Rules:

- never use color as the only signal for important state
- on colored surfaces, use the matching foreground token or opacity variant
- prefer semantic variants over raw palette classes in shared UI

#### Build palettes with enough shades

Most color systems need more steps than initially expected.

Useful defaults:

- 8 to 10 steps for neutrals
- 5 to 10 steps for each accent or brand family

That gives enough room for:

- surfaces
- borders
- hover states
- active states
- readable foregrounds

#### Greys do not have to be perfectly grey

Neutral colors can lean slightly warm or cool if that better fits the product
palette. The goal is a coherent interface, not mathematical neutrality.

#### Do not let lightness kill saturation

Very light tints often look washed out. If a surface needs subtle color, a
low-opacity saturated color frequently communicates better than a chalky pastel.

Use color sparingly on the face of the component. Accent borders and restrained
background washes are usually enough.

#### Do not use grey text on colored backgrounds

On colored surfaces, use:

- the matching foreground token
- a reduced-opacity version of that foreground when needed
- a hand-picked semantic token if the component requires one

Do not drop generic grey text onto colored or image-based surfaces.

#### Meet WCAG contrast ratios

Text must be readable:

- `4.5:1` for normal text
- `3:1` for large text

Accessible does not have to mean ugly. Build contrast into the palette instead
of treating it as a late-stage compromise.

#### Do not rely on color alone

Use copy, placement, icons, borders, or shape alongside color when
communicating:

- destructive state
- success
- warning
- selection
- disabled state

### 4c. Layout Grid and Spacing Scale

Use consistent spacing instead of one-off values. Page composition should read
as a rhythm, not as a pile of local tweaks.

Good current examples:

- `starter/internal/view/page/home.go`
- `starter/internal/view/page/contact.go`
- `pkg/web/viewstate/errorpage/errorpage.go`

#### Establish a spacing and sizing system

Use a small set of repeated values rather than inventing new spacing for every
surface. Repetition makes the interface feel intentional and makes component
composition faster.

#### Grids are tools, not laws

Do not force every layout into equal fluid columns.

Prefer:

- fixed or max-width sidebars when navigation needs stability
- constrained cards and forms that shrink only when necessary
- content-driven widths inside flexible containers

Use grid when it helps the content, not because a grid is available.

#### Relative sizing does not scale automatically

Do not assume that if body text shrinks by some ratio, headings, padding, and
adjacent elements should shrink by the same ratio.

In practice:

- large headings often need to shrink faster than body text on small screens
- button padding and font size should be tuned independently
- card and form spacing can stay comfortable even when type scales down a bit

#### Right-align numbers in tables

Numeric columns should be right-aligned so values of different magnitudes stay
comparable at a glance. Keep free text and mixed-content columns left-aligned.

### 4d. Elevation and Shadow System

Use these defaults:

- none or border-only for structural rows and sections
- `shadow-xs` for cards and small controls
- `shadow-md` for transient feedback
- stronger shadows for off-canvas drawers and modal overlays

#### Keep the implied light source consistent

The system assumes a conventional top-down light source. Avoid mixing shadow
directions or effects that imply conflicting lighting.

#### Use shadows to show elevation, not decoration

Think about z-axis intent, not about the shadow itself:

- buttons sit slightly above the background
- cards sit above the page
- dropdowns and dialogs sit above cards

If the element is not elevated in the interaction model, it probably does not
need a stronger shadow.

#### Overlap only when it clarifies layers

Layering is appropriate for:

- the mobile nav drawer
- dropdowns and popovers
- dialogs
- toasts

Do not overlap elements in normal page flow merely for visual novelty.

### 4e. Image Usage and Aspect Ratio Rules

Use images carefully and only when they have a job.

Rules:

- use good images, not generic filler
- preserve text contrast on top of images
- give images an intended display size
- treat user-uploaded images as hostile to layout consistency

#### Text over images needs deliberate contrast control

A photograph contains both light and dark areas. Solve text contrast by
reducing image dynamics with overlays, cropping, or blur rather than by hoping
one text color works everywhere.

#### Everything has an intended size

Icons, screenshots, and photos look best near the size they were designed to
be seen.

- small icons should usually stay small and gain presence through padding or a
  surrounding shape
- screenshots should be cropped, not crushed into unreadable thumbnails

#### Beware user-uploaded content

User images have unpredictable aspect ratios, backgrounds, and quality. Contain
them in fixed-size wrappers and crop with `object-fit: cover` when the layout
depends on consistency.

### 4f. Finishing Touches and Polish Patterns

Apply these only after hierarchy, spacing, and accessibility are already solid.

#### Supercharge the defaults

Before inventing a new component, ask whether the default HTML element or
existing component can carry a bit more personality through:

- better icon use
- stronger underlines
- improved grouping
- slightly richer state styling

#### Add color with accent borders

A thin accent border is one of the easiest ways to add intention without
hurting legibility.

Good uses:

- the top edge of a card or panel
- the left edge of an alert
- an active nav indicator
- a short underline beneath a heading

#### Use fewer borders

Borders are useful, but overuse creates noise.

Prefer:

- spacing for grouping
- contrast for hierarchy
- shadows for elevation
- borders for inputs, tables, and intentional separation

#### Backgrounds should support, not distract

Most screens should lean on:

- `bg-background` for the page
- `bg-card` for elevated surfaces
- `bg-muted` or `bg-accent` sparingly for supporting distinction

Decorative backgrounds are acceptable only when they do not weaken legibility
or fight the app's restrained personality.

#### Do not overlook empty states

Polish often shows up in the states teams postpone.

Every meaningful surface should consider:

- what appears when there is no data
- what appears while work is happening
- what appears when the operation fails

The home status cards and contact form pending state are the current model for this:

- `starter/internal/view/page/home.go`
- `starter/internal/view/partial/contactpartial/contact_form.go`

---

## 5. Component Library Guide

### 5a. Core UI Components (pkg/componentry/ui/core)

Use `ui/core` for the smallest shared primitives.

Primary components:

- `Button`
- `Badge`
- `Label`
- `Avatar`
- `Icon`
- `Separator`
- `Skeleton`
- `Popover`

Rules:

- use `core.Button` for actions instead of hand-rolled `<button>` classes
- use `core.Badge` for terse status or category indicators, not full feedback
- use `core.Label` directly or through form helpers instead of styling labels
  ad hoc
- soften icon contrast before increasing icon size when icons compete with text

### 5b. Button Variants and Usage Guidelines

Use variants consistently:

- `VariantDefault`: primary action
- `VariantSecondary`: lower-emphasis filled action
- `VariantOutline`: secondary action needing a boundary
- `VariantGhost`: quiet inline action
- `VariantDestructive`: dangerous primary action
- `VariantDestructiveGhost`: dangerous action that should stay visually quiet
- `VariantLink`: text-only navigation or action

Use sizes consistently:

- default for primary form and page actions
- `SizeSm` for row actions, pagination controls, and compact nav actions
- `SizeLg` only when the layout genuinely needs a larger target

### 5c. Data Display Components (pkg/componentry/ui/data)

Use `ui/data` for grouped informational surfaces and tabular display.

Primary components:

- `Card.Root`, `Card.Header`, `Card.Title`, `Card.Description`,
  `Card.Content`, `Card.Footer`
- `Table.Root`, `Table.Header`, `Table.Body`, `Table.Row`, `Table.Head`,
  `Table.Cell`, `Table.Caption`

Rules:

- use cards for bounded tasks, summaries, and empty states
- use tables for multi-column structured data, not for page layout
- keep table actions compact and aligned for scanning
- keep card padding inside card subcomponents, not wrapper `div` clutter

### 5d. Feedback Components (pkg/componentry/ui/feedback)

Use `ui/feedback` for feedback surfaces and progress, not for terse status
chips.

Primary components:

- `Alert`
- `Toast`
- `Progress`
- `Spinner`

Rules:

- alerts explain a situation in context
- toasts acknowledge an event and should stay brief
- destructive variants are for danger or failure, not generic emphasis
- preserve the accessibility roles and structure already encoded in the package

### 5e. Layout Components (pkg/componentry/ui/layout)

Use `ui/layout` for shell-level navigation and structural navigation patterns.

Primary components:

- `Navbar`
- `NavMenu`
- `Sidebar`

Rules:

- configure primary navigation declaratively through `NavConfig`
- reuse `Sidebar` and `NavMenu` behavior instead of building a second mobile
  drawer pattern
- push auth and theme differences through nav slots, not duplicated view logic

### 5f. Form Components (pkg/componentry/form)

Use `form` for accessible field composition and consistent input wiring.

Primary components and helpers:

- `Field`
- `Fieldset`
- `Input`
- `Textarea`
- `Select`
- `FileUpload`
- `Switch`
- `Toggle`
- `FieldControlAttrs`

Rules:

- use `Field` for label, control, description, hint, and error grouping
- use `FieldControlAttrs` so controls point at descriptions and errors
- prefer package defaults over hand-assembling error markup
- keep standalone forms narrow unless the task genuinely requires density

### 5g. Interactive Components (pkg/componentry/interactive)

Use `interactive/*` for higher-level UI that remains HTML-first and progressive.

Current examples:

- `accordion`
- `breadcrumb`
- `dialog`
- `dropdown`
- `pagination`
- `tabs`
- `tooltip`

Rules:

- prefer native HTML behavior where the package already encodes it
- keep interaction affordances consistent with the rest of the design language
- use these packages when behavior would otherwise be reimplemented in a page

### 5h. UI Patterns Library (pkg/componentry/patterns)

Use `patterns/*` for cross-cutting UI behavior and wiring rather than visual
primitives.

Important packages:

- `patterns/flash`
- `patterns/font`
- `patterns/form`
- `patterns/head`
- `patterns/htmx`
- `patterns/pager`
- `patterns/redirect`

Rules:

- use typed HTMX helpers instead of sprinkling ad hoc `hx-*` strings
- keep async behavior local to the markup it affects
- use flash and head helpers for app-wide conventions instead of view-local
  duplication

### 5i. Component Examples (pkg/showcase/componentry)

Treat `pkg/showcase/componentry` as the living visual reference for the
component library.

Use it to:

- review the intended default variants
- compare component families side by side
- anchor UI guide examples to real package usage

---

## 6. View Composition and Layout

### 6a. Layout Templates (pkg/web/viewstate/layout/)

`pkg/web/viewstate/layout/layout.go` is the shared application shell behind
`viewstate.Request.Page(...)`.

It is responsible for:

- document structure
- stylesheet and script inclusion
- primary nav rendering
- body-level CSRF and HTMX wiring
- flash toast container placement

Do not duplicate shell concerns in page-level views.

### 6b. Full Page Views (starter/internal/view/page/)

Use `starter/internal/view/page/` for full-page constructors.

Rules:

- accept `viewstate.Request` first
- wrap content with `req.Page(...)`
- compose page structure with shared components first, utilities second
- use utility classes for layout and spacing, not to recreate button, card, or
  alert systems
- keep semantic structure correct even when the visual styling is restrained

Current page patterns:

- `home.go`: centered landing composition with clear action hierarchy
- `contact.go`: focused form flow and supportive copy

### 6c. HTMX Partial Views (starter/internal/view/partial/)

Use `starter/internal/view/partial/` for HTMX-replaceable fragments.

Rules:

- partials should preserve the same visual language as the full page
- partials should be self-sufficient for the DOM region they replace
- return the same surface type after mutation whenever possible
- partials should not become denser or noisier than the page around them

Reference implementations:

- `contactpartial/contact_form.go`: fragment-safe form composition
- `pkg/web/viewstate/errorpage/errorpage.go`: full-page and partial parity
- `pkg/showcase/componentry/demo/demo.go`: isolated demo fragments for shared UI

### 6d. Error Page Views (pkg/web/viewstate/errorpage/)

Errors should look like part of the application, not fallback HTML.

Follow the existing pattern:

- constrained card surface
- clear title and HTTP badge
- inline alert with a user-safe message
- one obvious escape action
- optional technical detail behind disclosure in debug mode

Reference:

- `pkg/web/viewstate/errorpage/errorpage.go`

---

## 7. Practical Rules for New UI

### 7a. Use Existing Shared Components First

Do not hand-roll:

- button styling
- badge styling
- card framing
- table anatomy
- alert and toast structure
- nav shell structure
- accessible field error wiring
- common HTMX attribute patterns

Ad hoc utilities are acceptable for:

- page spacing
- responsive wrappers
- view-specific alignment
- one-off composition around shared components

### 7b. Prefer Composition over Variant Explosion

If a screen needs a special arrangement, compose existing primitives first.
Add a new component variant only when the same visual pattern is reused in
multiple places.

### 7c. Make Action Hierarchy Visually Obvious

Default action hierarchy:

- primary action: high-contrast filled button
- secondary action: outline or secondary filled treatment
- tertiary action: ghost or link treatment

Destructive actions do not automatically become the visual primary action. If a
dangerous action is not the main intended path, keep it quiet until the point
of no return.

### 7d. Keep Forms Readable and Accessible

Most forms in this app should follow this shape:

- constrained width when standalone
- `form.Field` for each control
- clear top-level error presentation where needed
- one obvious primary submit action
- quiet secondary navigation or cancellation

### 7e. Keep Table Row Actions Quiet Until Needed

For tabular data:

- data should dominate, controls should support
- prefer `ghost` for edit and view actions
- reserve destructive styling for real danger
- keep row actions in a right-aligned compact group

### 7f. Balance Text, Icons, and Borders

When one element feels too heavy:

- soften icon contrast before changing icon size
- increase border weight slightly before making the color harsher
- de-emphasize competing content before over-emphasizing the target

### 7g. Writing and Microcopy Are Part of the Design

Default copy style:

- direct
- plain
- helpful
- not overly playful
- not legalistic unless the domain requires it

Choose words that reduce friction and match the visual restraint of the system.

---

## 8. UI Decision Checklist

Before merging a UI change, confirm:

- the design starts from the feature, not from extra shell complexity
- a shared component or pattern was used where one already exists
- hierarchy comes from spacing, weight, contrast, and placement before extra
  color
- semantic tokens were used instead of arbitrary palette classes
- widths are constrained where readability matters
- focus, invalid, and feedback states are visible
- action hierarchy is obvious without reading every label twice
- grouping is clear because spacing around groups is larger than spacing within
  them
- the mobile layout still works without inventing a second visual language
- HTMX partials match the full-page design language
- the screen has credible empty, loading, and error states where applicable
- text on colored or image backgrounds meets contrast requirements
- labels in display UI are quieter than the values they describe
- numeric table columns are right-aligned
- headings use tighter tracking and leading than body text
- shadows reflect z-axis intent, not decoration
- any new palette extension defines enough shades before use

---

## 9. Reference Map

Use these as the practical source of truth:

- `pkg/showcase/componentry/`
- `github.com/go-sum/foundry/pkg/componentry/ui/core` (button, badge, label)
- `github.com/go-sum/foundry/pkg/componentry/ui/data` (card, table)
- `github.com/go-sum/foundry/pkg/componentry/ui/feedback` (alert, toast, progress, spinner)
- `github.com/go-sum/foundry/pkg/componentry/ui/layout` (navmenu, navbar, sidebar)
- `github.com/go-sum/foundry/pkg/componentry/form` (field, fieldset, file upload)
- `github.com/go-sum/foundry/pkg/componentry/interactive/pagination`
- `github.com/go-sum/foundry/pkg/componentry/patterns/htmx`
- `pkg/web/viewstate/layout/layout.go`
- `pkg/web/viewstate/errorpage/errorpage.go`
- `starter/internal/view/page/home.go`
- `starter/internal/view/page/contact.go`
- `starter/internal/view/partial/contactpartial/contact_form.go`

When this guide and the code diverge, update the guide quickly. UI guidance is
only useful if it describes the UI that actually exists.
