Require Import Arith.

Theorem diff_goal : forall (n m : nat), n + m = m + n.
Proof.
  intros n m.
  rewrite Nat.add_comm.
  reflexivity.
Qed.
