(import "env" "wavm_halt_and_set_finished" (func $wavm_halt_and_set_finished))

(func $main (local f64)
	;; abs, neg
	(f64.const -1)
	(f64.abs)
	(call $assert_f64 (f64.const 1))
	(f64.neg)
	(call $assert_f64 (f64.const -1))
	(drop)

	;; ceil
	(f64.const -0.8)
	(f64.ceil)
	(call $assert_f64 (f64.const -0))
	(f64.ceil)
	(call $assert_f64 (f64.const -0))
	(drop)

	;; floor
	(f64.const -0.8)
	(f64.floor)
	(call $assert_f64 (f64.const -1))
	(f64.floor)
	(call $assert_f64 (f64.const -1))
	(drop)

	;; trunc
	(f64.const -0.8)
	(f64.trunc)
	(call $assert_f64 (f64.const -0))
	(f64.trunc)
	(call $assert_f64 (f64.const -0))
	(drop)

	;; nearest
	(f64.const -0.8)
	(f64.nearest)
	(call $assert_f64 (f64.const -1))
	(f64.nearest)
	(call $assert_f64 (f64.const -1))
	(drop)

	;; sqrt
	(f64.const 123.456)
	(f64.sqrt)
	(call $assert_f64 (f64.const 11.111075555498667))
	(f64.sqrt)
	(call $assert_f64 (f64.const 3.3333279999872))
	(drop)

	;; add, sub, mul, div
	(f64.const 1.5)
	(f64.const 2)
	(f64.add)
	(call $assert_f64 (f64.const 3.5))
	(f64.const 5)
	(f64.sub)
	(call $assert_f64 (f64.const -1.5))
	(f64.const 100)
	(f64.mul)
	(call $assert_f64 (f64.const -150))
	(f64.const 7)
	(f64.div)
	(call $assert_f64 (f64.const -21.428571428571427))
	(drop)

	;; min, max, copysign
	(f64.const 5)
	(f64.const -2)
	(f64.min)
	(call $assert_f64 (f64.const -2))
	(f64.const -5)
	(f64.max)
	(call $assert_f64 (f64.const -2))
	(f64.const 1)
	(f64.copysign)
	(call $assert_f64 (f64.const 2))
	(drop)

	;; eq
	(f64.const 5)
	(f64.const 5)
	(f64.eq)
	(call $assert_true)
	(f64.const -5)
	(f64.const 5)
	(f64.eq)
	(call $assert_false)
	(f64.const 1)
	(f64.const 0)
	(f64.div)
	(local.tee 0)
	(local.get 0)
	(f64.sub)
	(local.tee 0)
	(local.get 0)
	(f64.eq)
	(call $assert_false)

	;; ne
	(local.get 0)
	(local.get 0)
	(f64.ne)
	(call $assert_true)
	(f64.const 1)
	(f64.const 1)
	(f64.ne)
	(call $assert_false)
	(f64.const 1)
	(f64.const 2)
	(f64.ne)
	(call $assert_true)

	;; lt
	(f64.const 1)
	(f64.const 2)
	(f64.lt)
	(call $assert_true)
	(f64.const 2)
	(f64.const 2)
	(f64.lt)
	(call $assert_false)
	(f64.const 3)
	(f64.const 2)
	(f64.lt)
	(call $assert_false)

	;; gt
	(f64.const 1)
	(f64.const 2)
	(f64.gt)
	(call $assert_false)
	(f64.const 2)
	(f64.const 2)
	(f64.gt)
	(call $assert_false)
	(f64.const 3)
	(f64.const 2)
	(f64.gt)
	(call $assert_true)

	;; le
	(f64.const 1)
	(f64.const 2)
	(f64.le)
	(call $assert_true)
	(f64.const 2)
	(f64.const 2)
	(f64.le)
	(call $assert_true)
	(f64.const 3)
	(f64.const 2)
	(f64.le)
	(call $assert_false)

	;; ge
	(f64.const 1)
	(f64.const 2)
	(f64.ge)
	(call $assert_false)
	(f64.const 2)
	(f64.const 2)
	(f64.ge)
	(call $assert_true)
	(f64.const 3)
	(f64.const 2)
	(f64.ge)
	(call $assert_true)

	;; f64 -> i32 truncation
	(f64.const -2.5)
	(i32.trunc_sat_f64_s)
	(call $assert_i32 (i32.const -2))
	(f64.const -2.5)
	(i32.trunc_sat_f64_u)
	(call $assert_i32 (i32.const 0))
	(f64.const 1000000000000)
	(i32.trunc_sat_f64_u)
	(i32.const -1)
	(call $assert_i32)
	(f64.const 1000000000000)
	(i32.trunc_sat_f64_s)
	(i32.const 1)
	(i32.shl (i32.const 63))
	(i32.sub (i32.const 1))
	(call $assert_i32)
	(f64.const 1000000000000)
	(i32.trunc_sat_f64_s)
	(i32.gt_s (i32.const 0))
	(call $assert_true)
	(f64.const -1000000000000)
	(i32.trunc_sat_f64_s)
	(i32.const 1)
	(i32.shl (i32.const 63))
	(call $assert_i32)
	(f64.const -1000000000000)
	(i32.trunc_sat_f64_s)
	(i32.lt_s (i32.const 0))
	(call $assert_true)

	;; f64 -> i64 truncation
	(f64.const -2.5)
	(i64.trunc_sat_f64_s)
	(call $assert_i64 (i64.const -2))
	(f64.const -2.5)
	(i64.trunc_sat_f64_u)
	(call $assert_i64 (i64.const 0))
	(f64.const 1000000000000000000000)
	(i64.trunc_sat_f64_u)
	(i64.const -1)
	(call $assert_i64)
	(f64.const 1000000000000000000000)
	(i64.trunc_sat_f64_s)
	(i64.const 1)
	(i64.shl (i64.const 63))
	(i64.sub (i64.const 1))
	(call $assert_i64)
	(f64.const 1000000000000000000000)
	(i64.trunc_sat_f64_s)
	(i64.gt_s (i64.const 0))
	(call $assert_true)
	(f64.const -1000000000000000000000)
	(i64.trunc_sat_f64_s)
	(i64.const 1)
	(i64.shl (i64.const 63))
	(call $assert_i64)
	(f64.const -1000000000000000000000)
	(i64.trunc_sat_f64_s)
	(i64.lt_s (i64.const 0))
	(call $assert_true)

	;; f32<->f64 promotion/demotion
	(f32.const -123)
	(f64.promote_f32)
	(call $assert_f64 (f64.const -123))
	(f32.demote_f64)
	(call $assert_f32 (f32.const -123))
	(drop)

	;; now for my favorite floating point test
	(f64.const 0.1)
	(f64.const 0.2)
	(f64.add)
	(call $assert_f64 (f64.const 0.30000000000000004))
	(drop)

	(call $wavm_halt_and_set_finished)
)

(func $assert_f32 (param f32 f32) (result f32)
	(local.get 0)
	(i32.reinterpret_f32)
	(local.get 1)
	(i32.reinterpret_f32)
	(i32.sub)
	(local.get 0)
	(local.get 1)
	(f32.ne)
	(i32.or)
	(if
		(then
			;; push the params back on the stack for debugging
			(local.get 0)
			(local.get 1)
			unreachable
		)
	)
	(local.get 0)
)

(func $assert_f64 (param f64 f64) (result f64)
	(local.get 0)
	(i64.reinterpret_f64)
	(local.get 1)
	(i64.reinterpret_f64)
	(i64.sub)
	(i64.eqz)
	(i32.eqz)
	(local.get 0)
	(local.get 1)
	(f64.ne)
	(i32.or)
	(if
		(then
			;; push the params back on the stack for debugging
			(local.get 0)
			(local.get 1)
			unreachable
		)
	)
	(local.get 0)
)

(func $assert_i32 (param i32 i32)
	(local.get 0)
	(local.get 1)
	(i32.sub)
	(if
		(then
			;; push the params back on the stack for debugging
			(local.get 0)
			(local.get 1)
			unreachable
		)
	)
)

(func $assert_i64 (param i64 i64)
	(local.get 0)
	(local.get 1)
	(i64.sub)
	(i64.eqz)
	(i32.eqz)
	(if
		(then
			;; push the params back on the stack for debugging
			(local.get 0)
			(local.get 1)
			unreachable
		)
	)
)

(func $assert_true (param i32)
	(local.get 0)
	(i32.eqz)
	(if
		(then
			(local.get 0)
			unreachable
		)
	)
)

(func $assert_false (param i32)
	(local.get 0)
	(if
		(then
			(local.get 0)
			unreachable
		)
	)
)

(start $main)
