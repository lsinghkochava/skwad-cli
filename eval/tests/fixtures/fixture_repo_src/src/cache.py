"""A small least-recently-used (LRU) cache for the fixture repo."""

from collections import OrderedDict


class LRUCache:
    """Least-recently-used cache with a fixed capacity.

    NOTE (relied on by the non-obvious canary): eviction is genuinely LRU, not
    FIFO. ``get`` calls ``move_to_end`` so the most-recently-accessed key is
    protected; ``evict`` drops the *front* (least-recently-used) entry.
    """

    def __init__(self, capacity):
        self.capacity = capacity
        self._store = OrderedDict()

    def get(self, key):
        if key not in self._store:
            return None
        # Touch on read so recency reflects access, not just insertion.
        self._store.move_to_end(key)
        return self._store[key]

    def put(self, key, value):
        self._store[key] = value
        self._store.move_to_end(key)
        if len(self._store) > self.capacity:
            self.evict()

    def evict(self):
        """Remove the least-recently-used entry (front of the OrderedDict)."""
        self._store.popitem(last=False)
