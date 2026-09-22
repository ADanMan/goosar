import { describe, it, expect } from 'vitest';
import { createNewestFirstFollow, FOLLOW_EDGE_THRESHOLD } from './transcript-follow';

function makeFollow(startAt = 0) {
  let t = startAt;
  const follow = createNewestFirstFollow(() => t);
  const tick = (ms: number) => {
    t += ms;
  };
  follow.setActive(true);
  return { follow, tick };
}

describe('createNewestFirstFollow', () => {
  it('starts following and pins system displacement back to the live end', () => {
    const { follow, tick } = makeFollow();
    tick(1000);
    expect(follow.onScroll(500)).toBe(true);
    expect(follow.isFollowing()).toBe(true);
  });

  it('does not disengage when a prepend shift lands inside the input window after an in-zone nudge', () => {
    const { follow, tick } = makeFollow();
    tick(1000);
    follow.input(30);
    tick(200); 
    expect(follow.onScroll(500)).toBe(false); 
    expect(follow.isFollowing()).toBe(true);
    tick(400); 
    expect(follow.onScroll(500)).toBe(true); 
    expect(follow.isFollowing()).toBe(true);
  });

  it('disengages on accumulated user input beyond the threshold', () => {
    const { follow } = makeFollow();
    follow.input(80);
    expect(follow.isFollowing()).toBe(true);
    follow.input(80); 
    expect(follow.isFollowing()).toBe(false);
    expect(follow.onScroll(400)).toBe(false); 
  });

  it('upward input rolls the accumulator back instead of counting as intent', () => {
    const { follow } = makeFollow();
    follow.input(100);
    follow.input(-90);
    follow.input(100); 
    expect(follow.isFollowing()).toBe(true);
  });

  it('a pin clears sub-threshold residue so old nudges cannot accumulate', () => {
    const { follow, tick } = makeFollow();
    follow.input(100);
    tick(1000);
    expect(follow.onScroll(300)).toBe(true); 
    follow.input(100); 
    expect(follow.isFollowing()).toBe(true);
  });

  it('re-engages when the viewport returns to the top zone', () => {
    const { follow } = makeFollow();
    follow.input(FOLLOW_EDGE_THRESHOLD + 1);
    expect(follow.isFollowing()).toBe(false);
    follow.onAtTopChange(true);
    expect(follow.isFollowing()).toBe(true);
  });

  it('scrollbar drag past the threshold disengages', () => {
    const { follow } = makeFollow();
    follow.pointerDown(true);
    expect(follow.onScroll(80)).toBe(false); 
    expect(follow.isFollowing()).toBe(true);
    follow.onScroll(200);
    expect(follow.isFollowing()).toBe(false);
    follow.pointerUp();
  });

  it('never pins while the mouse is held on row content (text selection autoscroll)', () => {
    const { follow, tick } = makeFollow();
    tick(1000);
    follow.pointerDown(false); 
    expect(follow.onScroll(600)).toBe(false);
    expect(follow.isFollowing()).toBe(true); 
    follow.pointerUp();
    expect(follow.onScroll(600)).toBe(true); 
  });

  it('explicit disengage (segment navigation) stops the pinning', () => {
    const { follow, tick } = makeFollow();
    follow.disengage();
    tick(1000);
    expect(follow.isFollowing()).toBe(false);
    expect(follow.onScroll(300)).toBe(false);
  });

  it('reset re-engages and clears held-pointer state', () => {
    const { follow, tick } = makeFollow();
    follow.pointerDown(false);
    follow.input(500);
    follow.reset();
    tick(1000);
    expect(follow.isFollowing()).toBe(true);
    expect(follow.onScroll(50)).toBe(true); 
  });

  it('is fully inert when inactive (chronological or completed task)', () => {
    const { follow, tick } = makeFollow();
    follow.setActive(false);
    tick(1000);
    expect(follow.isFollowing()).toBe(false);
    expect(follow.onScroll(500)).toBe(false);
    follow.input(500); 
    follow.setActive(true);
    expect(follow.isFollowing()).toBe(true); 
  });
});
